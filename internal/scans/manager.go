package scans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/config"
	"github.com/filipegalo/shelly-energy-dashboard/internal/credentials"
	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
	"github.com/filipegalo/shelly-energy-dashboard/internal/metrics"
	"github.com/filipegalo/shelly-energy-dashboard/internal/networks"
	"github.com/filipegalo/shelly-energy-dashboard/internal/shelly"
)

type hub struct {
	latest      database.Scan
	subscribers map[chan database.Scan]struct{}
}

type Manager struct {
	appCtx  context.Context
	store   *database.Store
	cfg     config.Config
	logger  *slog.Logger
	metrics *metrics.Metrics
	cipher  *credentials.Cipher
	probe   func(context.Context, database.Network, netip.Addr) (shelly.DeviceInfo, error)

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
	hubs    map[int64]*hub
	wg      sync.WaitGroup
	slots   chan struct{}
}

func NewManager(appCtx context.Context, store *database.Store, cfg config.Config, logger *slog.Logger, metricSet *metrics.Metrics, cipher *credentials.Cipher) *Manager {
	maxScans := cfg.MaxConcurrentScans
	if maxScans < 1 {
		maxScans = 4
	}
	manager := &Manager{appCtx: appCtx, store: store, cfg: cfg, logger: logger, metrics: metricSet, cipher: cipher, cancels: map[int64]context.CancelFunc{}, hubs: map[int64]*hub{}, slots: make(chan struct{}, maxScans)}
	manager.probe = manager.probeAddress
	return manager
}

func (m *Manager) Start(ctx context.Context, networkID int64, trigger string) (database.Scan, error) {
	network, err := m.store.GetNetwork(ctx, networkID)
	if err != nil {
		return database.Scan{}, err
	}
	if !network.Enabled {
		return database.Scan{}, errors.New("network scanning is disabled")
	}
	if err := validateNetworkRuntime(network); err != nil {
		return database.Scan{}, err
	}
	prefix, err := networks.ValidateCIDR(network.CIDR, m.cfg.AllowNonPrivateNetworks, m.cfg.MaxSubnetPrefix)
	if err != nil {
		return database.Scan{}, err
	}
	hosts, err := networks.UsableHosts(prefix, m.cfg.MaxHosts)
	if err != nil {
		return database.Scan{}, err
	}
	if m.cfg.EnableMDNS {
		hosts = appendMDNSCandidates(ctx, hosts, prefix)
	}
	if !m.acquireSlot() {
		return database.Scan{}, errors.New("global concurrent scan limit reached")
	}
	scan, err := m.store.CreateScan(ctx, networkID, trigger, len(hosts))
	if err != nil {
		m.releaseSlot()
		return database.Scan{}, err
	}
	scanCtx, cancel := context.WithCancel(m.appCtx)
	m.mu.Lock()
	m.cancels[scan.ID] = cancel
	m.hubs[scan.ID] = &hub{latest: scan, subscribers: map[chan database.Scan]struct{}{}}
	m.mu.Unlock()
	m.wg.Add(1)
	go m.run(scanCtx, scan, network, hosts)
	return scan, nil
}

func (m *Manager) StartManual(ctx context.Context, networkID int64, target netip.Addr) (database.Scan, error) {
	network, err := m.store.GetNetwork(ctx, networkID)
	if err != nil {
		return database.Scan{}, err
	}
	if !network.Enabled {
		return database.Scan{}, errors.New("network scanning is disabled")
	}
	if err := validateNetworkRuntime(network); err != nil {
		return database.Scan{}, err
	}
	prefix, err := networks.ValidateCIDR(network.CIDR, m.cfg.AllowNonPrivateNetworks, m.cfg.MaxSubnetPrefix)
	if err != nil || !prefix.Contains(target) {
		return database.Scan{}, errors.New("target is outside the selected enabled network")
	}
	if !m.acquireSlot() {
		return database.Scan{}, errors.New("global concurrent scan limit reached")
	}
	scan, err := m.store.CreateScan(ctx, networkID, "manual_device", 1)
	if err != nil {
		m.releaseSlot()
		return database.Scan{}, err
	}
	scanCtx, cancel := context.WithCancel(m.appCtx)
	m.mu.Lock()
	m.cancels[scan.ID] = cancel
	m.hubs[scan.ID] = &hub{latest: scan, subscribers: map[chan database.Scan]struct{}{}}
	m.mu.Unlock()
	m.wg.Add(1)
	go m.run(scanCtx, scan, network, []netip.Addr{target})
	return scan, nil
}

type probeResult struct {
	address  netip.Addr
	info     shelly.DeviceInfo
	err      error
	duration time.Duration
}

func (m *Manager) run(ctx context.Context, scan database.Scan, network database.Network, hosts []netip.Addr) {
	defer m.wg.Done()
	defer m.releaseSlot()
	started := time.Now()
	m.metrics.RunningScans.Inc()
	defer m.metrics.RunningScans.Dec()
	if err := m.store.StartScan(context.Background(), scan.ID); err != nil {
		m.finish(scan, "failed", err.Error(), started, nil, false)
		return
	}
	scan.Status = "running"
	startTime := time.Now().UTC()
	scan.StartedAt = &startTime
	m.publish(scan)

	workerCount := network.ScanConcurrency
	if workerCount < 1 {
		workerCount = m.cfg.DefaultScanConcurrency
	}
	results := runProbePool(ctx, hosts, workerCount, func(probeCtx context.Context, address netip.Addr) (shelly.DeviceInfo, error) {
		return m.probe(probeCtx, network, address)
	})

	seenIDs := make([]int64, 0)
	var lastError string
	for result := range results {
		scan.AddressesScanned++
		m.metrics.AddressesScanned.Inc()
		if result.err == nil || errors.Is(result.err, shelly.ErrAuthenticationRequired) {
			auth := errors.Is(result.err, shelly.ErrAuthenticationRequired)
			upserted, err := m.store.UpsertDevice(context.Background(), network.ID, result.address.String(), result.info, auth, errorText(result.err))
			if err == nil {
				scan.DevicesFound++
				m.metrics.DevicesDiscovered.Inc()
				seenIDs = append(seenIDs, upserted.Device.ID)
				if upserted.New {
					scan.NewDevicesFound++
					m.metrics.NewDevices.Inc()
				} else {
					scan.UpdatedDevices++
				}
			} else {
				scan.ErrorsCount++
				lastError = "database update failed"
				m.logger.Error("device inventory update failed", "scan_id", scan.ID, "network_id", network.ID, "target_ip", result.address.String(), "operation", "upsert_device", "error", err)
			}
			m.metrics.APIRequestDuration.WithLabelValues("shelly").Observe(result.duration.Seconds())
		} else if !errors.Is(result.err, context.Canceled) {
			scan.ErrorsCount++
			lastError = classifyError(result.err)
			m.metrics.APIFailures.WithLabelValues(lastError).Inc()
			m.metrics.APIRequestDuration.WithLabelValues("failure").Observe(result.duration.Seconds())
		}
		if lastError != "" {
			scan.ErrorSummary = lastError
		}
		if scan.AddressesScanned%8 == 0 || scan.AddressesScanned == scan.AddressesTotal {
			_ = m.store.UpdateScanProgress(context.Background(), scan)
			m.publish(scan)
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		m.finish(scan, "cancelled", "scan cancelled", started, nil, false)
		return
	}
	status := "completed"
	if scan.ErrorsCount > 0 {
		status = "completed_with_errors"
	}
	fullNetworkScan := scan.TriggerType == "manual" || scan.TriggerType == "scheduled"
	m.finish(scan, status, scan.ErrorSummary, started, seenIDs, fullNetworkScan)
}

func (m *Manager) finish(scan database.Scan, status, message string, started time.Time, seenIDs []int64, fullNetworkScan bool) {
	scan.Status, scan.ErrorSummary = status, message
	completed := time.Now().UTC()
	scan.CompletedAt = &completed
	var persistErr error
	if fullNetworkScan {
		persistErr = m.store.FinalizeFullScan(context.Background(), scan, seenIDs, m.cfg.OfflineFailureThreshold)
	} else {
		persistErr = m.store.CompleteScan(context.Background(), scan)
	}
	if persistErr != nil {
		m.logger.Error("persist terminal scan state", "scan_id", scan.ID, "network_id", scan.NetworkID, "operation", "complete_scan", "result", status, "error", persistErr)
		if fullNetworkScan {
			scan.Status = "failed"
			scan.ErrorSummary = "failed to finalize device lifecycle"
			if fallbackErr := m.store.CompleteScan(context.Background(), scan); fallbackErr != nil {
				m.logger.Error("persist failed scan fallback", "scan_id", scan.ID, "network_id", scan.NetworkID, "operation", "complete_scan_fallback", "error", fallbackErr)
			}
		}
	}
	m.metrics.ScanCount.WithLabelValues(scan.Status).Inc()
	m.metrics.ScanDuration.WithLabelValues(scan.Status).Observe(time.Since(started).Seconds())
	m.publish(scan)
	m.mu.Lock()
	delete(m.cancels, scan.ID)
	m.mu.Unlock()
	m.logger.Info("discovery scan finished", "scan_id", scan.ID, "network_id", scan.NetworkID, "operation", "scan", "duration", time.Since(started), "result", status)
}

func (m *Manager) Cancel(scanID int64) error {
	m.mu.Lock()
	cancel, ok := m.cancels[scanID]
	m.mu.Unlock()
	if !ok {
		return errors.New("scan is not running")
	}
	cancel()
	return nil
}

func (m *Manager) Subscribe(ctx context.Context, scanID int64) (<-chan database.Scan, func(), error) {
	channel := make(chan database.Scan, 4)
	m.mu.Lock()
	h, ok := m.hubs[scanID]
	if !ok {
		scan, err := m.store.GetScan(ctx, scanID)
		if err != nil {
			m.mu.Unlock()
			return nil, nil, err
		}
		h = &hub{latest: scan, subscribers: map[chan database.Scan]struct{}{}}
		m.hubs[scanID] = h
	}
	h.subscribers[channel] = struct{}{}
	latest := h.latest
	m.mu.Unlock()
	channel <- latest
	unsubscribe := func() {
		m.mu.Lock()
		if current, exists := m.hubs[scanID]; exists {
			delete(current.subscribers, channel)
			if len(current.subscribers) == 0 && current.latest.Status != "queued" && current.latest.Status != "running" {
				delete(m.hubs, scanID)
			}
		}
		m.mu.Unlock()
	}
	return channel, unsubscribe, nil
}

func (m *Manager) publish(scan database.Scan) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hubs[scan.ID]
	if !ok {
		h = &hub{subscribers: map[chan database.Scan]struct{}{}}
		m.hubs[scan.ID] = h
	}
	h.latest = scan
	for channel := range h.subscribers {
		select {
		case channel <- scan:
		default:
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- scan:
			default:
			}
		}
	}
	if len(h.subscribers) == 0 && scan.Status != "queued" && scan.Status != "running" {
		delete(m.hubs, scan.ID)
	}
}

func (m *Manager) Wait() { m.wg.Wait() }

func (m *Manager) acquireSlot() bool {
	select {
	case m.slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (m *Manager) releaseSlot() { <-m.slots }

func (m *Manager) credentialForAddress(ctx context.Context, networkID int64, address string) *shelly.Credential {
	if m.cipher == nil {
		return nil
	}
	username, encrypted, err := m.store.DeviceCredentialForAddress(ctx, networkID, address)
	if err != nil {
		return nil
	}
	password, err := m.cipher.Decrypt(encrypted)
	if err != nil {
		return nil
	}
	return &shelly.Credential{Username: username, Password: password}
}

func (m *Manager) probeAddress(ctx context.Context, network database.Network, address netip.Addr) (shelly.DeviceInfo, error) {
	client := shelly.NewClient(time.Duration(network.ConnectionTimeoutMS)*time.Millisecond, time.Duration(network.RequestTimeoutMS)*time.Millisecond)
	return client.Probe(ctx, address, m.credentialForAddress(ctx, network.ID, address.String()))
}

func validateNetworkRuntime(network database.Network) error {
	if network.ScanConcurrency < 1 || network.ScanConcurrency > 256 {
		return errors.New("scan concurrency is outside safe limits")
	}
	if network.ConnectionTimeoutMS < 50 || network.ConnectionTimeoutMS > 10000 {
		return errors.New("connection timeout is outside safe limits")
	}
	if network.RequestTimeoutMS < 100 || network.RequestTimeoutMS > 30000 {
		return errors.New("request timeout is outside safe limits")
	}
	return nil
}

func classifyError(err error) string {
	if errors.Is(err, shelly.ErrResponseTooLarge) {
		return "oversized"
	}
	if errors.Is(err, shelly.ErrNotShelly) {
		return "not_shelly"
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "timeout"
	}
	return "request"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, shelly.ErrAuthenticationRequired) {
		return "authentication required"
	}
	return fmt.Sprintf("%v", err)
}

var _ = sql.ErrNoRows

type probeFunc func(context.Context, netip.Addr) (shelly.DeviceInfo, error)

func runProbePool(ctx context.Context, hosts []netip.Addr, concurrency int, probe probeFunc) <-chan probeResult {
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan netip.Addr)
	results := make(chan probeResult, concurrency)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for address := range jobs {
				begin := time.Now()
				info, err := probe(ctx, address)
				select {
				case results <- probeResult{address: address, info: info, err: err, duration: time.Since(begin)}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, host := range hosts {
			select {
			case jobs <- host:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	return results
}

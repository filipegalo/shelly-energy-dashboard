package shelly

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const defaultMaxBody = 1 << 20

var (
	ErrAuthenticationRequired = errors.New("device requires authentication")
	ErrNotShelly              = errors.New("response is not a recognized Shelly device")
	ErrResponseTooLarge       = errors.New("device response exceeds size limit")
)

type Credential struct {
	Username string
	Password string
}

type Client struct {
	ConnectionTimeout time.Duration
	RequestTimeout    time.Duration
	MaxBodyBytes      int64
	dialContext       func(context.Context, string, string) (net.Conn, error)
}

func NewClient(connectionTimeout, requestTimeout time.Duration) *Client {
	return &Client{ConnectionTimeout: connectionTimeout, RequestTimeout: requestTimeout, MaxBodyBytes: defaultMaxBody}
}

// Identify reads only Shelly's public identity endpoint and never sends a
// credential. Callers can pin a stable device identity before authenticated
// polling when DHCP addresses may be reassigned.
func (c *Client) Identify(ctx context.Context, address netip.Addr) (DeviceInfo, error) {
	if !address.Is4() {
		return DeviceInfo{}, errors.New("Shelly identification requires a literal IPv4 address")
	}
	raw, err := c.get(ctx, c.httpClient(address), address, "/shelly", nil)
	if err != nil {
		return DeviceInfo{}, err
	}
	if !resemblesShelly(raw) {
		return DeviceInfo{}, ErrNotShelly
	}
	return Normalize(raw, nil, nil)
}

func (c *Client) Probe(ctx context.Context, address netip.Addr, credential *Credential) (DeviceInfo, error) {
	if !address.Is4() {
		return DeviceInfo{}, errors.New("Shelly probe requires a literal IPv4 address")
	}
	client := c.httpClient(address)
	infoRaw, err := c.get(ctx, client, address, "/shelly", credential)
	if err != nil {
		if errors.Is(err, ErrAuthenticationRequired) {
			return DeviceInfo{AuthenticationRequired: true}, err
		}
		return DeviceInfo{}, err
	}
	if !resemblesShelly(infoRaw) {
		return DeviceInfo{}, ErrNotShelly
	}
	generation := detectGeneration(infoRaw)
	var configRaw, statusRaw json.RawMessage
	authRequired := false
	if generation <= 1 {
		configRaw, err = c.get(ctx, client, address, "/settings", credential)
		authRequired = authRequired || errors.Is(err, ErrAuthenticationRequired)
		statusRaw, err = c.get(ctx, client, address, "/status", credential)
		authRequired = authRequired || errors.Is(err, ErrAuthenticationRequired)
	} else {
		if value, rpcErr := c.get(ctx, client, address, "/rpc/Sys.GetConfig", credential); rpcErr == nil {
			configRaw = value
		} else if value, rpcErr = c.get(ctx, client, address, "/rpc/Shelly.GetDeviceInfo", credential); rpcErr == nil {
			configRaw = value
		} else if errors.Is(rpcErr, ErrAuthenticationRequired) {
			authRequired = true
		}
		if value, rpcErr := c.get(ctx, client, address, "/rpc/Shelly.GetStatus", credential); rpcErr == nil {
			statusRaw = value
		} else if value, rpcErr = c.get(ctx, client, address, "/rpc/Sys.GetStatus", credential); rpcErr == nil {
			statusRaw = value
		} else if errors.Is(rpcErr, ErrAuthenticationRequired) {
			authRequired = true
		}
	}
	info, err := Normalize(infoRaw, configRaw, statusRaw)
	if err != nil {
		return DeviceInfo{}, err
	}
	if authRequired {
		info.AuthenticationRequired = true
		return info, ErrAuthenticationRequired
	}
	return info, nil
}

func (c *Client) httpClient(target netip.Addr) *http.Client {
	dialer := net.Dialer{Timeout: c.ConnectionTimeout, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: c.RequestTimeout,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			parsed, err := netip.ParseAddr(host)
			if err != nil || parsed != target {
				return nil, errors.New("blocked unexpected Shelly connection destination")
			}
			if c.dialContext != nil {
				return c.dialContext(ctx, network, address)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(target.String(), port))
		},
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       c.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *Client) get(ctx context.Context, client *http.Client, target netip.Addr, path string, credential *Credential) (json.RawMessage, error) {
	url := "http://" + target.String() + path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		challenge := response.Header.Get("WWW-Authenticate")
		if credential == nil || challenge == "" {
			return nil, ErrAuthenticationRequired
		}
		response.Body.Close()
		request, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		var authorization string
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "basic") {
			authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte(credential.Username+":"+credential.Password))
		} else {
			authorization, err = digestAuthorization(http.MethodGet, path, challenge, *credential)
			if err != nil {
				return nil, ErrAuthenticationRequired
			}
		}
		request.Header.Set("Authorization", authorization)
		request.Header.Set("Accept", "application/json")
		response, err = client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, ErrAuthenticationRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Shelly endpoint returned HTTP %d", response.StatusCode)
	}
	limit := c.MaxBodyBytes
	if limit <= 0 {
		limit = defaultMaxBody
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	if !json.Valid(body) {
		return nil, errors.New("device returned invalid JSON")
	}
	return json.RawMessage(body), nil
}

func resemblesShelly(raw json.RawMessage) bool {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	_, hasID := value["id"]
	_, hasMAC := value["mac"]
	_, hasModel := value["model"]
	_, hasType := value["type"]
	_, hasGen := value["gen"]
	_, hasFirmware := value["fw"]
	_, hasAuth := value["auth"]
	modern := hasID && (hasMAC || hasModel || hasType || hasGen)
	gen1 := hasMAC && hasType && (hasFirmware || hasAuth)
	return modern || gen1
}

func detectGeneration(raw json.RawMessage) int {
	var value struct {
		Gen int `json:"gen"`
	}
	if json.Unmarshal(raw, &value) == nil && value.Gen > 0 {
		return value.Gen
	}
	return 1
}

func digestAuthorization(method, uri, challenge string, credential Credential) (string, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest ") {
		return "", errors.New("unsupported authentication challenge")
	}
	params := parseDigestParams(strings.TrimSpace(challenge[len("Digest "):]))
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" {
		return "", errors.New("invalid digest challenge")
	}
	algorithm := strings.ToUpper(params["algorithm"])
	if algorithm == "" {
		algorithm = "MD5"
	}
	if algorithm != "MD5" && algorithm != "MD5-SESS" && algorithm != "SHA-256" && algorithm != "SHA-256-SESS" {
		return "", errors.New("unsupported digest algorithm")
	}
	offeredQOP := strings.ToLower(params["qop"])
	if offeredQOP != "" && !offersDigestAuth(offeredQOP) {
		return "", errors.New("digest challenge does not offer qop=auth")
	}
	hash := func(value string) string {
		if strings.HasPrefix(algorithm, "SHA-256") {
			sum := sha256.Sum256([]byte(value))
			return hex.EncodeToString(sum[:])
		}
		sum := md5.Sum([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	ha1 := hash(credential.Username + ":" + realm + ":" + credential.Password)
	ha2 := hash(method + ":" + uri)
	cnonceBytes := make([]byte, 12)
	if _, err := rand.Read(cnonceBytes); err != nil {
		return "", err
	}
	cnonce := hex.EncodeToString(cnonceBytes)
	nc, qop := "00000001", "auth"
	if strings.HasSuffix(algorithm, "-SESS") {
		ha1 = hash(ha1 + ":" + nonce + ":" + cnonce)
	}
	response := ""
	authorization := ""
	if offeredQOP == "" {
		response = hash(ha1 + ":" + nonce + ":" + ha2)
		authorization = fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=%s, response="%s"`, quoteDigest(credential.Username), quoteDigest(realm), quoteDigest(nonce), quoteDigest(uri), algorithm, response)
	} else {
		response = hash(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
		authorization = fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=%s, response="%s", qop=%s, nc=%s, cnonce="%s"`, quoteDigest(credential.Username), quoteDigest(realm), quoteDigest(nonce), quoteDigest(uri), algorithm, response, qop, nc, cnonce)
	}
	if opaque := params["opaque"]; opaque != "" {
		authorization += fmt.Sprintf(`, opaque="%s"`, quoteDigest(opaque))
	}
	return authorization, nil
}

func offersDigestAuth(raw string) bool {
	for _, value := range strings.Split(raw, ",") {
		if strings.TrimSpace(value) == "auth" {
			return true
		}
	}
	return false
}

func parseDigestParams(raw string) map[string]string {
	result := map[string]string{}
	for len(raw) > 0 {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, ","))
		equals := strings.IndexByte(raw, '=')
		if equals < 1 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(raw[:equals]))
		raw = strings.TrimSpace(raw[equals+1:])
		var value string
		if strings.HasPrefix(raw, `"`) {
			raw = raw[1:]
			end := strings.IndexByte(raw, '"')
			if end < 0 {
				break
			}
			value, raw = raw[:end], raw[end+1:]
		} else {
			end := strings.IndexByte(raw, ',')
			if end < 0 {
				value, raw = raw, ""
			} else {
				value, raw = raw[:end], raw[end:]
			}
		}
		result[key] = strings.TrimSpace(value)
	}
	return result
}

func quoteDigest(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

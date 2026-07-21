(() => {
  const root = document.getElementById('scan-progress');
  if (!root) return;
  const source = new EventSource(`/events/scans/${root.dataset.scanId}`);
  source.addEventListener('progress', (event) => {
    const scan = JSON.parse(event.data);
    const percent = scan.addresses_total ? Math.floor(scan.addresses_scanned * 100 / scan.addresses_total) : 0;
    document.getElementById('progress-percent').textContent = `${percent}%`;
    document.getElementById('progress-bar').style.width = `${percent}%`;
    document.getElementById('addresses-scanned').textContent = scan.addresses_scanned;
    document.getElementById('devices-found').textContent = scan.devices_found;
    document.getElementById('new-found').textContent = scan.new_devices_found;
    document.getElementById('updated-found').textContent = scan.updated_devices;
    document.getElementById('errors-count').textContent = scan.errors_count;
    const status = document.getElementById('progress-status');
    status.textContent = scan.status;
    status.className = `status status-${scan.status.replaceAll('_', '-')}`;
    if (!['queued', 'running'].includes(scan.status)) {
      source.close();
      document.querySelector('.cancel-scan-form')?.remove();
    }
  });
})();

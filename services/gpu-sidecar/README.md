# recasa-gpu-sidecar

Exposes NVIDIA GPU stats as JSON on `:28640/gpu-stats` for the CasaOS GPU
dashboard widget. CasaOS itself has no GPU support, so this shells out to
`nvidia-smi` on every request instead. Installed as a systemd service
(`recasa-gpu-sidecar.service`), independent of the CasaOS services proper.

Response shape:
```json
{
  "utilization_percent": 0,
  "memory_used_mib": 0,
  "memory_total_mib": 11264,
  "temperature_c": 38,
  "power_draw_w": 16.08,
  "power_limit_w": 250,
  "processes": [{"pid": 1234, "command": "python3", "utilization_percent": 12}]
}
```

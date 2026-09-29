# Changelog

Notable user-facing changes to NivaroOS. (`services/core/CHANGELOG.md` is the
inherited CasaOS history and is not kept up to date.)

## Unreleased

### Added

- Fan control (Settings > Fans & Cooling, and a Fans page in the app): live
  fan speeds and CPU/GPU temperatures, per-fan Auto / Fixed / Curve modes
  with a drag-to-edit curve, Quiet / Balanced / Performance / Auto
  profiles, renaming, and an on-demand Identify step. Works on any PC with
  what it finds: motherboard super-I/O chips (the driver is found and
  loaded by the installer), AMD GPUs, laptop fan drivers and the Raspberry
  Pi fan through hwmon, NVIDIA GPUs through NVML. Safety first: never below
  20% (or the GPU's own minimum), 100% on every driven fan at the emergency
  temperature (CPU 85 °C / GPU 83 °C by default), a stalled fan or a lost
  sensor goes back to Auto, and every fan goes back to BIOS/driver control
  whenever the service stops, crashes or the machine shuts down. Fans that
  can't be driven (ACPI resource conflict, no driver, read-only driver) are
  shown read-only with the reason.

### Changed

- New NivaroOS logo, "Ni": an N whose last stroke doubles as an i, with a
  mint status dot, on a blue (#2563EB) tile. It replaces the old CasaOS
  three-circle cloud mark everywhere: web UI favicon, PWA/home-screen icons,
  sign-in and first-run screens, Settings > About, the app launch screen,
  the Download Station browser's new tab, the Android app's launcher icon
  (adaptive and Android 13 themed icon) and in-app mark, and the README.
  Source files, colours and usage rules: [`docs/brand/`](docs/brand/README.md).

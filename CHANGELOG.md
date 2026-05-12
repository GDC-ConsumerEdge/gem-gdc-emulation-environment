# Changelog

## [0.1.1](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.0...gem-v0.1.1) (2026-05-12)


### Features

* optimize vxlan ip address assignment and add GEM tunnel ([#4](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/4)) ([8610d0d](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/8610d0dc2c50eaa9cea266766fd8a512d1bfad32))
* **tunnel:** implement service name support and fix volumebindingmode incompatibility ([#8](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/8)) ([9386042](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/9386042b1bae76ea98b5705de253687916b7dee6))


### Bug Fixes

* **networking:** optimize k8s cluster node networking ([#6](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/6)) ([821ec11](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/821ec11d30db7800700339374834204fc45262c5))

## 0.1.0 (2026-05-11)

### Features

* **networking:** optimize vxlan ip address assignment and add GEM tunnel (#4)

### Bug Fixes

* **networking:** move to default vxlan interface name for cluster default network
* **pre-commit:** configure default stages to prevent duplicate pre-commit runs

### Documentation & Styling

* **readme:** update README and add GEM logo to admin workstation MOTD (#5)

### Maintenance & Chores

* **ci:** add required environment variable validation
* **os:** bump underlying OS to more recent LTS version (Ubuntu 24.04)
* **workflows:** add GitHub workflows for automated PR validation (#3)
* **initial:** initial repository commit and infrastructure baseline

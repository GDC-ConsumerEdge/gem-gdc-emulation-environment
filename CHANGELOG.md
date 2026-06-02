# Changelog

## [0.1.5](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.4...gem-v0.1.5) (2026-06-02)


### Features

* **networking:** implement persistent VXLAN overlay and additional documentation ([#16](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/16)) ([4c5cf9d](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/4c5cf9d58da4bd878578c39ec8cdfada3d7b40db))

## [0.1.4](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.3...gem-v0.1.4) (2026-05-29)


### Features

* **cloudbuild:** implement cloud build pipelines for cluster builds and deletions" ([#14](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/14)) ([6f17db9](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/6f17db99d62169fb96cfc87c00a33e57c6e708dd))

## [0.1.3](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.2...gem-v0.1.3) (2026-05-22)


### Bug Fixes

* **cluster:** cluster build optimizations ([#12](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/12)) ([1825c44](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/1825c44b871cf48d88f1ef0354ae29bf71b77a4a))

## [0.1.2](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.1...gem-v0.1.2) (2026-05-18)


### Features

* **gvisor:** configure cluster to support gvisor and runc ([8714190](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/8714190b95b966ef7fd08d206544f4dc85862253))
* optimize vxlan ip address assignment and add GEM tunnel ([#4](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/4)) ([8610d0d](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/8610d0dc2c50eaa9cea266766fd8a512d1bfad32))
* **tunnel:** implement service name support and fix volumebindingmode incompatibility ([#8](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/8)) ([9386042](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/9386042b1bae76ea98b5705de253687916b7dee6))


### Bug Fixes

* **networking:** optimize k8s cluster node networking ([#6](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/6)) ([821ec11](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/821ec11d30db7800700339374834204fc45262c5))
* **project-setup:** update repo path env variable ([6fe77a9](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/6fe77a9c1c0e7d0f132c37057e1fd1bd599f73e9))

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

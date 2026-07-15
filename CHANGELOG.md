# Changelog

## [0.1.10](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.9...gem-v0.1.10) (2026-07-15)


### Features

* **ci:** implement Github workflow for cluster build and conformance test validation ([16ed906](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/16ed906a5c2225606530a01345b246f85aebbf46))
* **gem:** add support for GDC G2 hardware emulation ([#28](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/28)) ([9ab1b9c](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/9ab1b9cb28b91f5a687112b4b7b89ff68859d88f))


### Bug Fixes

* **ci:** fix corrupted workflow yaml syntax ([80e904c](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/80e904c26efea5806eafe7c89c2b678047421f88))

## [0.1.9](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.8...gem-v0.1.9) (2026-06-09)


### Features

* **config:** update to support gdc version 1.13.0 ([#24](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/24)) ([4976005](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/497600555b146b9dc534d32bc35d67c1fc6e5f77))

## [0.1.8](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.7...gem-v0.1.8) (2026-06-04)


### Bug Fixes

* **tests:** update storageclass test to ensure valid k8s resource name ([471bca9](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/471bca93146ac3ab0b83d97b7e158e5ba9da2a3c))

## [0.1.7](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.6...gem-v0.1.7) (2026-06-04)


### Bug Fixes

* **workstation:** always publish SSH public key to instance metadata ([#21](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/21)) ([067f274](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/067f2743f7fe99fa2e7dd72e4d2d79e62884e0cc))

## [0.1.6](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/compare/gem-v0.1.5...gem-v0.1.6) (2026-06-03)


### Features

* impersonate provisioning SA by default in all Terraform modules ([#19](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/issues/19)) ([da564e6](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment/commit/da564e6eebdc5430bfd034653a638de0402e4dd5))

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

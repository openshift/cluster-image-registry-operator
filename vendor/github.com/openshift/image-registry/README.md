OpenShift Image Registry
========================

[![Go Report Card](https://goreportcard.com/badge/github.com/openshift/image-registry)](https://goreportcard.com/report/github.com/openshift/image-registry)
[![GoDoc](https://godoc.org/github.com/openshift/image-registry?status.svg)](https://godoc.org/github.com/openshift/image-registry)
[![Build Status](https://travis-ci.org/openshift/origin.svg?branch=master)](https://travis-ci.org/openshift/origin)
[![Coverage Status](https://coveralls.io/repos/github/openshift/image-registry/badge.svg?branch=master)](https://coveralls.io/github/openshift/image-registry?branch=master)
[![Licensed under Apache License version 2.0](https://img.shields.io/github/license/openshift/image-registry.svg?maxAge=2592000)](https://www.apache.org/licenses/LICENSE-2.0)

***OpenShift Image Registry*** is a tightly integrated with [OpenShift Origin](https://www.openshift.org/) application that lets you distribute Docker images.

Installation and configuration instructions can be found in the
[OpenShift documentation](https://docs.okd.io/latest/install_config/registry/index.html).

**Features:**

* Pull and cache images from remote registries.
* Role-based access control (RBAC).
* Audit log.
* Prometheus metrics.

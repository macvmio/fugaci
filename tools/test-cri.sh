#!/bin/bash

#crictl pull registry.k8s.io/e2e-test-images/httpd:2.4.39-4
crictl pull registry.k8s.io/e2e-test-images/busybox:1.29-2

critest --ginkgo.focus "runtime should support basic operations on container"


#critest --ginkgo.focus "runtime should support starting container with log"

#critest --ginkgo.focus "container" --ginkgo.skip "Security Context" --ginkgo.skip "Container Mount Propagation" --ginkgo.skip "runtime should support networking" --ginkgo.skip "runtime should support adding volume and device"


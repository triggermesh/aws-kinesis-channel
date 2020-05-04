# Copyright 2020 TriggerMesh, Inc
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
# 
#     http://www.apache.org/licenses/LICENSE-2.0
# 
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Code generation
#
# see https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md#generate-code

# Name of the Go package for this repository
PKG := github.com/triggermesh/aws-kinesis-channel

# List of API groups to generate code for
# e.g. "sources/v1alpha1 sources/v1alpha2"
API_GROUPS := messaging/v1alpha1
# generates e.g. "PKG/pkg/apis/sources/v1alpha1 PKG/pkg/apis/sources/v1alpha2"
api-import-paths := $(foreach group,$(API_GROUPS),$(PKG)/pkg/apis/$(group))

generators := deepcopy client lister informer injection

.PHONY: codegen $(generators)

codegen: $(generators)

# http://blog.jgc.org/2007/06/escaping-comma-and-space-in-gnu-make.html
comma := ,
space :=
space +=

deepcopy:
	@echo "+ Generating deepcopy funcs for $(API_GROUPS)"
	@go run k8s.io/code-generator/cmd/deepcopy-gen \
		--go-header-file hack/boilerplate/boilerplate.go.txt \
		--input-dirs $(subst $(space),$(comma),$(api-import-paths))

client:
	@echo "+ Generating clientsets for $(API_GROUPS)"
	@rm -rf pkg/client/clientset
	@go run k8s.io/code-generator/cmd/client-gen \
		--go-header-file hack/boilerplate/boilerplate.go.txt \
		--input $(subst $(space),$(comma),$(API_GROUPS)) \
		--input-base $(PKG)/pkg/apis \
		--output-package $(PKG)/pkg/client/clientset

lister:
	@echo "+ Generating listers for $(API_GROUPS)"
	@rm -rf pkg/client/listers
	@go run k8s.io/code-generator/cmd/lister-gen \
		--go-header-file hack/boilerplate/boilerplate.go.txt \
		--input-dirs $(subst $(space),$(comma),$(api-import-paths)) \
		--output-package $(PKG)/pkg/client/listers

informer:
	@echo "+ Generating informers for $(API_GROUPS)"
	@rm -rf pkg/client/informers
	@go run k8s.io/code-generator/cmd/informer-gen \
		--go-header-file hack/boilerplate/boilerplate.go.txt \
		--input-dirs $(subst $(space),$(comma),$(api-import-paths)) \
		--output-package $(PKG)/pkg/client/informers \
		--versioned-clientset-package $(PKG)/pkg/client/clientset/internalclientset \
		--listers-package $(PKG)/pkg/client/listers

injection:
	@echo "+ Generating injection for $(API_GROUPS)"
	@rm -rf pkg/client/injection
	@go run knative.dev/pkg/codegen/cmd/injection-gen \
		--go-header-file hack/boilerplate/boilerplate.go.txt \
		--input-dirs $(subst $(space),$(comma),$(api-import-paths)) \
		--output-package $(PKG)/pkg/client/injection \
		--versioned-clientset-package $(PKG)/pkg/client/clientset/internalclientset \
		--listers-package $(PKG)/pkg/client/listers \
		--external-versions-informers-package $(PKG)/pkg/client/informers/externalversions
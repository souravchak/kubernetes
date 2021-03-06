#!/bin/bash

# Copyright 2014 Google Inc. All rights reserved.
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

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"

kube::golang::setup_env
"${KUBE_ROOT}/hack/build-go.sh" cmd/gendocs cmd/genman cmd/genbashcomp

# Find binary
gendocs=$(kube::util::find-binary "gendocs")
genman=$(kube::util::find-binary "genman")
genbashcomp=$(kube::util::find-binary "genbashcomp")

if [[ ! -x "$gendocs" || ! -x "$genman" || ! -x "$genbashcomp" ]]; then
  {
    echo "It looks as if you don't have a compiled gendocs, genman, or genbashcomp binary"
    echo
    echo "If you are running from a clone of the git repo, please run"
    echo "'./hack/build-go.sh cmd/gendocs cmd/genman cmd/genbashcomp'."
  } >&2
  exit 1
fi

kube::util::gen-doc "${gendocs}" "${KUBE_ROOT}/docs/" '###### Auto generated by spf13/cobra'
kube::util::gen-doc "${genman}" "${KUBE_ROOT}/docs/man/man1"
kube::util::gen-doc "${genbashcomp}" "${KUBE_ROOT}/contrib/completions/bash/"

# ex: ts=2 sw=2 et filetype=sh

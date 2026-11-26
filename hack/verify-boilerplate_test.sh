#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
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

# Tests for hack/verify-boilerplate.sh. Each case builds a throwaway git
# repository, runs the script on it and compares the exit code and the exact
# output. The last case runs the script on this repository.
#
# Usage: hack/verify-boilerplate_test.sh

set -o errexit -o nounset -o pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
subject="$script_dir/verify-boilerplate.sh"
repo_root=$(cd "$script_dir/.." && pwd)

work=$(mktemp -d "${TMPDIR:-/tmp}/verify-boilerplate.XXXXXX")
trap 'rm -rf "$work"' EXIT

failures=0

# hash_header YEAR prints the canonical header for files with "#" comments.
hash_header() {
	cat <<EOF
# SPDX-FileCopyrightText: Copyright (c) $1 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
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
EOF
}

# go_header YEAR prints the canonical header for Go files.
go_header() {
	cat <<EOF
/*
 * SPDX-FileCopyrightText: Copyright (c) $1 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
EOF
}

# new_repo creates an empty git repository under $work and prints its path.
new_repo() {
	local dir
	dir=$(mktemp -d "$work/repo.XXXXXX")
	git -C "$dir" init -q
	echo "$dir"
}

# put DIR PATH writes stdin to DIR/PATH and stages it.
put() {
	mkdir -p "$(dirname "$1/$2")"
	cat >"$1/$2"
	git -C "$1" add -- "$2"
}

# indent prefixes each line of stdin so expected and actual output stand out.
indent() {
	sed 's/^/    | /'
}

# fail_summary BAD CHECKED EXCLUDED prints the last line of a failing run.
fail_summary() {
	echo "FAIL: $1 of $2 files checked, $3 excluded. Copy the header from a file of the same type, or add an exclusion with a reason to hack/verify-boilerplate.sh."
}

# expect NAME WANT_RC WANT_OUTPUT DIR runs the script on DIR and compares the
# exit code and the combined stdout and stderr with the expected values.
expect() {
	local name=$1 want_rc=$2 want_out=$3 dir=$4 got_out got_rc=0
	got_out=$("$subject" "$dir" 2>&1) || got_rc=$?
	if [[ $got_rc -eq $want_rc && $got_out == "$want_out" ]]; then
		echo "ok   $name"
		return
	fi
	failures=$((failures + 1))
	echo "FAIL $name"
	echo "  want rc=$want_rc, output:"
	echo "$want_out" | indent
	echo "  got rc=$got_rc, output:"
	echo "$got_out" | indent
}

# bad NAME PATH STYLE CONTENT expects a repository holding only PATH, with
# CONTENT, to fail against the STYLE template. CONTENT is an argument, not
# stdin, so expect runs in this shell and its failure count is kept.
bad() {
	local dir
	dir=$(new_repo)
	printf '%s\n' "$4" | put "$dir" "$2"
	expect "$1" 1 "$2: header differs from the $3 template
$(fail_summary 1 1 0)" "$dir"
}

# Canonical headers pass, including after a shebang, with a single year and
# with a year range. Excluded files need no header and are counted apart.
dir=$(new_repo)
{ go_header 2024; echo; echo 'package main'; } | put "$dir" cmd/main.go
{ echo '#!/usr/bin/env bash'; hash_header 2024-2026; echo 'echo hi'; } | put "$dir" run.sh
{ hash_header 2026; echo 'key: value'; } | put "$dir" config.yaml
{ hash_header 2023; echo 'all:'; } | put "$dir" Makefile
echo '# Title' | put "$dir" README.md
echo 'kind: {{ .Values.kind }}' | put "$dir" charts/demo/templates/deployment.yaml
expect "canonical headers pass" 0 "OK: 4 files checked, 2 excluded." "$dir"

# Files without the header fail.
bad "missing header fails" config.yaml '#' 'key: value'
bad "Go file without header fails" main.go Go 'package main'

# The header must start on the first line, or right after the shebang.
dir=$(new_repo)
{ echo '#!/bin/bash'; echo; hash_header 2024; } | put "$dir" run.sh
{ echo; hash_header 2024; } | put "$dir" job.yml
{ echo; go_header 2024; } | put "$dir" main.go
expect "header after a blank line fails" 1 "job.yml: header differs from the # template
main.go: header differs from the Go template
run.sh: header differs from the # template
$(fail_summary 3 3 0)" "$dir"

# Year forms other than YYYY and YYYY-YYYY fail.
for year in '2024, 2026' '2024 - 2026' '24' '2026-' '(2026)'; do
	bad "year form \"$year\" fails" config.yaml '#' "$(hash_header "$year")"
done

# Any other change to the text fails: the Apache URL must be indented by four
# spaces, the copyright holder must match, and Go files use the block form,
# "/*" first, " * " on every header line and " */" last.
bad "unindented URL fails (#)" config.yaml '#' "$(hash_header 2026 | sed 's|^#     http|# http|')"
bad "unindented URL fails (Go)" main.go Go "$(go_header 2026 | sed 's|^ \*     http| * http|')"
bad "wrong copyright holder fails" config.yaml '#' "$(hash_header 2026 | sed '1s|NVIDIA CORPORATION & AFFILIATES|NVIDIA Corporation|')"
bad "Go header with a \"#\" copyright line fails" main.go Go "$(go_header 2026 | sed '2s|^ \* |# |')"
bad "Go header opened with /** fails" main.go Go "$(go_header 2026 | sed '1s|^/\*$|/**|')"
bad "Go header without \" */\" fails" main.go Go "$(go_header 2026 | sed '$d'; echo 'package main')"

# A tracked file deleted from the work tree has nothing to check.
dir=$(new_repo)
{ hash_header 2026; echo 'key: value'; } | put "$dir" config.yaml
echo 'key: value' | put "$dir" gone.yaml
rm "$dir/gone.yaml"
expect "tracked file deleted from the work tree is skipped" 0 "OK: 1 files checked, 0 excluded." "$dir"

# A file type with no rule of its own is covered, staged or not: values.yaml
# is outside the excluded chart templates and tests, and new.toml is untracked.
dir=$(new_repo)
echo 'replicas: 1' | put "$dir" charts/demo/values.yaml
mkdir -p "$dir/tools"
echo 'key = "value"' >"$dir/tools/new.toml"
echo 'kind: {{ .Values.kind }}' | put "$dir" charts/demo/templates/deployment.yaml
expect "non-excluded new files fail" 1 "charts/demo/values.yaml: header differs from the # template
tools/new.toml: header differs from the # template
$(fail_summary 2 2 1)" "$dir"

# A directory that is not a git work tree, or one with nothing to check, is an
# error rather than a vacuous pass.
dir="$work/not-a-repo"
mkdir -p "$dir"
expect "not a git work tree is an error" 2 "verify-boilerplate: $dir is not a git work tree" "$dir"

dir=$(new_repo)
expect "empty repository is an error" 2 "verify-boilerplate: no files to check in $dir" "$dir"

# This repository passes.
got_rc=0
got_out=$("$subject" "$repo_root" 2>&1) || got_rc=$?
if [[ $got_rc -eq 0 && $got_out == OK:* ]]; then
	echo "ok   this repository passes ($got_out)"
else
	failures=$((failures + 1))
	echo "FAIL this repository passes"
	echo "$got_out" | indent
fi

if [[ $failures -ne 0 ]]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo "all cases passed"

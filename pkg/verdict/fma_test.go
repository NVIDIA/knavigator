/*
 * Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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

package verdict_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// listingLine matches an instruction line of a -gcflags=-S listing:
// "\t0x0c28 03112 (/path/fairshare.go:224)\tFMADDD\tF1, F2, F0, F3". The
// path may contain spaces, so it is matched up to ".go:<line>)".
var listingLine = regexp.MustCompile(`\((.+\.go):(\d+)\)\t(\S+)`)

// fusedSites returns "file.go:line INSTR" for every instruction in listing
// whose mnemonic matches fused, and how many instruction lines came from
// fairshare.go.
func fusedSites(listing string, fused *regexp.Regexp) ([]string, int) {
	var out []string
	fromFairshare := 0
	for _, m := range listingLine.FindAllStringSubmatch(listing, -1) {
		if filepath.Base(m[1]) == "fairshare.go" {
			fromFairshare++
		}
		if fused.MatchString(m[3]) {
			out = append(out, filepath.Base(m[1])+":"+m[2]+" "+m[3])
		}
	}
	return out, fromFairshare
}

// TestNoFusedMultiplyAdd compiles this package for two architectures and
// fails on any fused multiply-add instruction. Go may fuse x*y+z into one
// rounding (arm64 always may; amd64 may with GOAMD64=v3), and a fused result
// can differ in the last bit from the separately rounded one, so the same
// event log would give different measurements on two machines. Every
// product that reaches a sum is rounded by an explicit float64(...); this
// checks the compiled code, so a new unwrapped product fails here even when
// no fixture's arithmetic shows the difference.
func TestNoFusedMultiplyAdd(t *testing.T) {
	goBin, err := exec.LookPath("go")
	require.NoError(t, err, "the go command must be on PATH")
	tests := []struct {
		name  string
		env   []string
		fused string
	}{
		{name: "arm64", env: []string{"GOARCH=arm64"}, fused: `^FN?M(ADD|SUB)[DS]$`},
		{name: "amd64 with GOAMD64=v3", env: []string{"GOARCH=amd64", "GOAMD64=v3"}, fused: `^VFN?M(ADD|SUB)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// a library package with -o /dev/null writes nothing; the
			// listing is replayed from the build cache when it is warm
			cmd := exec.Command(goBin, "build", "-o", os.DevNull, "-gcflags=-S", ".")
			cmd.Env = append(os.Environ(), tc.env...)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			listing := string(out)
			// an empty or partial listing must not pass
			require.Contains(t, listing, "pkg/verdict.FairShare STEXT")
			require.Contains(t, listing, "pkg/verdict.Preemption STEXT")
			sites, fromFairshare := fusedSites(listing, regexp.MustCompile(tc.fused))
			require.NotZero(t, fromFairshare,
				"the -gcflags=-S listing could not be parsed: no instruction line is attributed to fairshare.go")
			require.Empty(t, sites)
		})
	}
}

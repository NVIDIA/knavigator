/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/oklog/run"
	"gopkg.in/yaml.v3"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
)

type Server struct {
	s   *http.Server
	log *logr.Logger
}

type WorkflowHandler struct {
	eng *engine.Eng
}

func New(log *logr.Logger, eng *engine.Eng, port int) *Server {
	mux := http.NewServeMux()
	mux.Handle("/workflow", &WorkflowHandler{eng: eng})

	return &Server{
		log: log,
		s: &http.Server{ // #nosec G112 // Potential Slowloris Attack because ReadHeaderTimeout is not configured in the http.Server
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
	}
}

func (srv *Server) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var g run.Group
	// Signal handler
	g.Add(run.SignalHandler(ctx, os.Interrupt, syscall.SIGTERM))
	// Server
	g.Add(
		func() error {
			srv.log.Info("Starting server", "address", srv.s.Addr)
			return srv.s.ListenAndServe()
		},
		func(err error) {
			srv.log.Error(err, "Stopping server")
			if err := srv.s.Shutdown(ctx); err != nil {
				srv.log.Error(err, "Error during server shutdown")
			}
			srv.log.Info("Server stopped")
		})

	return g.Run()
}

func (h *WorkflowHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close() //nolint:errcheck // No check for the return value of Body.Close()

	var workflow config.Workflow
	if err = yaml.Unmarshal(body, &workflow); err != nil {
		http.Error(w, "Invalid YAML format", http.StatusBadRequest)
		return
	}

	if err = engine.Run(r.Context(), h.eng, &workflow); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

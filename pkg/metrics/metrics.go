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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	nodeResourceRequests  *prometheus.GaugeVec
	nodeResourceLimits    *prometheus.GaugeVec
	nodeResourceOccupancy *prometheus.GaugeVec
)

func New(nodeLabels []string) {
	labels := append([]string{"node", "resource"}, nodeLabels...)

	nodeResourceRequests = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_resource_requests",
			Help: "Gauge of node resource requests.",
		}, labels)

	nodeResourceLimits = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_resource_limits",
			Help: "Gauge of node resource limits.",
		}, labels)

	nodeResourceOccupancy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_resource_occupancy",
			Help: "Occupancy percentage of node resource.",
		}, labels)
}

#!/usr/bin/env python3
import os
import matplotlib as mpl
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

# --- Configuration: Names, Mappings, Colors ---
FONT_SIZE = 16
OUTPUT_DIRECTORY = "results_gang_scheduling"
SHEET_NAME = "Gang scheduling"

CONFIG_ORDER = {
    "heterogeneous": ["Standard load", "Extensive load"],
    "homogeneous": [
        "Small cluster, fine-grained",
        "Small cluster, coarse-grained",
        "Big cluster, fine-grained",
        "Big cluster, coarse-grained",
    ],
}

METRIC_CONFIG = {
    "Turnaround time [s]": {
        "internal_key": "TurnaroundTime",
        "plot_ylabel": "Time [s]",
        "conversion_factor": 1.0,
        "plot_title_metric_name": "Average Turnaround Time"
    },
    "Avg. CPU utilization [%]": {
        "internal_key": "AvgCPUUtilization",
        "plot_ylabel": "Avg. CPU Utilization [%]",
        "conversion_factor": 1.0,
        "plot_title_metric_name": "Average CPU Utilization"
    },
    "Avg. memory utilization [%]": {
        "internal_key": "AvgMemoryUtilization",
        "plot_ylabel": "Avg. Memory Utilization [%]",
        "conversion_factor": 1.0,
        "plot_title_metric_name": "Average Memory Utilization"
    },
    "Mean running pods count": {
        "internal_key": "MeanRunningPodsCount",
        "plot_ylabel": "Count",
        "conversion_factor": 1.0,
        "plot_title_metric_name": "Mean Running Pods Count"
    }
}

COMBINED_METRICS_PLOTS = {
    "CPU_Memory_Utilization": {
        "metrics_to_combine": ["Avg. CPU utilization [%]", "Avg. memory utilization [%]"],
        "internal_key": "CPUMemoryUtilization",
        "plot_ylabel": "Utilization [%]",
        "plot_title_main_name": "Avg. CPU and Memory Utilization"
    }
}

PLOT_TASKS = [
    {"type": "single", "metric_excel_key": "Turnaround time [s]"},
    {"type": "single", "metric_excel_key": "Mean running pods count"},
    {"type": "combined", "config_key": "CPU_Memory_Utilization"}
]

VARIANT_TYPES = ["heterogeneous", "homogeneous"]


def hex_to_rgba(hex_color, alpha):
    rgb = mpl.colors.hex2color(hex_color)
    return (*rgb, alpha)


ALPHA_VALUE = 1
EDGE_COLOR = 'black'
LINE_WIDTH = 1.0
HATCH_CPU = '/'
HATCH_MEMORY = '.'

SYSTEM_MAIN_COLORS_HEX = {
    'Kueue': 'blue',
    'Volcano': 'red',
    'Yunikorn': 'green'
}
SYSTEM_MAIN_COLORS = {k: hex_to_rgba(v, ALPHA_VALUE) for k, v in SYSTEM_MAIN_COLORS_HEX.items()}
# SYSTEM_MAIN_COLORS['Kueue'] = (0.498, 0.498, 1.0, 1.0)
# SYSTEM_MAIN_COLORS['Volcano'] = (1, 0.498, 0.498, 1.0)
# SYSTEM_MAIN_COLORS['Yunikorn'] = (0.498, 0.745, 0.498, 1.0)

try:
    raw_df = pd.read_excel("Wyniki.xlsx", sheet_name=SHEET_NAME)
except FileNotFoundError:
    print("Error: File Wyniki.xlsx not found.")
    exit()
except ValueError as e:
    if "Sheet_name" in str(e) or "sheet" in str(e).lower():
        print(f"Error: Sheet '{SHEET_NAME}' not found in Wyniki.xlsx.")
        exit()
    raise e

blocks = []
base_excel_headers = [
    "Variant", "Configuration", "System", "Metric", "Run 1", "Run 2", "Run 3", "Run 4", "Run 5",
    "Average (calculated)", "Std. deviation (calculated)"
]
essential_columns_for_plotting = [
    "Variant", "Configuration", "System", "Metric", "Average (calculated)", "Std. deviation (calculated)"
]

for suffix in ["", ".1", ".2", ".3"]:
    current_excel_cols_block = [f"{col}{suffix}" for col in base_excel_headers]
    if not current_excel_cols_block[0] in raw_df.columns:
        continue
    current_essential_cols_with_suffix = [f"{col}{suffix}" for col in essential_columns_for_plotting]
    if any(col not in raw_df.columns for col in current_essential_cols_with_suffix):
        continue
    if any(col not in raw_df.columns for col in current_excel_cols_block):
        blk = raw_df[current_essential_cols_with_suffix].copy()
        blk.columns = essential_columns_for_plotting
    else:
        blk = raw_df[current_excel_cols_block].copy()
        blk.columns = base_excel_headers
        blk = blk[essential_columns_for_plotting].copy()
    blocks.append(blk)

if not blocks:
    print(f"Error: No data blocks loaded from sheet '{SHEET_NAME}'.")
    exit()

data = pd.concat(blocks, ignore_index=True)
data = data.dropna(subset=["Variant", "Configuration", "System", "Metric"])
for header_name in essential_columns_for_plotting:
    data = data[data[header_name] != header_name]

data["Average (calculated)"] = pd.to_numeric(data["Average (calculated)"], errors="coerce")
data["Std. deviation (calculated)"] = pd.to_numeric(data["Std. deviation (calculated)"], errors="coerce")
data = data.dropna(subset=["Average (calculated)"])
data["Std. deviation (calculated)"] = data["Std. deviation (calculated)"].fillna(0)
data["Variant"] = data["Variant"].str.lower().str.strip()
data["Configuration"] = data["Configuration"].str.strip()
data["System"] = data["System"].str.strip()
data["Metric"] = data["Metric"].str.strip()


def plot_configuration_comparison(ax, data_for_plot, x_axis_configs_order, metrics_excel_keys,
                                  current_system_name, current_variant_name_raw,
                                  plot_file_name_suffix_arg=""):  # Added plot_file_name_suffix_arg
    n_configs_on_x = len(x_axis_configs_order)
    x_indices = np.arange(n_configs_on_x)
    num_metrics_in_plot = len(metrics_excel_keys)
    total_width_per_group = 0.7
    bar_width = total_width_per_group / num_metrics_in_plot if num_metrics_in_plot > 1 else total_width_per_group

    legend_handles_for_plot = []
    all_plot_means = []
    all_plot_stds = []

    for i, metric_key_excel in enumerate(metrics_excel_keys):
        metric_definition = METRIC_CONFIG[metric_key_excel]
        current_metric_means = pd.Series(index=x_axis_configs_order, dtype=float)
        current_metric_stds = pd.Series(index=x_axis_configs_order, dtype=float)

        for config_name_on_x in x_axis_configs_order:
            row_data = data_for_plot[
                (data_for_plot["Configuration"] == config_name_on_x) & (data_for_plot["Metric"] == metric_key_excel)
                ]
            if not row_data.empty and pd.notna(row_data["Average (calculated)"].iloc[0]):
                current_metric_means[config_name_on_x] = row_data["Average (calculated)"].iloc[0] * metric_definition[
                    "conversion_factor"]
                current_metric_stds[config_name_on_x] = row_data["Std. deviation (calculated)"].iloc[0] * \
                                                        metric_definition["conversion_factor"]

        current_metric_means = current_metric_means.fillna(0)
        current_metric_stds = current_metric_stds.fillna(0)
        all_plot_means.extend(current_metric_means.values)
        all_plot_stds.extend(current_metric_stds.values)

        bar_positions = x_indices if num_metrics_in_plot == 1 else x_indices - (total_width_per_group / 2) + (
                    i * bar_width) + (bar_width / 2)
        bar_color = SYSTEM_MAIN_COLORS.get(current_system_name, hex_to_rgba('grey', ALPHA_VALUE))
        bar_hatch = None

        is_individual_homogeneous_cluster_util = (
                current_variant_name_raw == "homogeneous" and
                plot_file_name_suffix_arg == "ClusterUtilization" and
                num_metrics_in_plot == 1  # This function will receive only one metric in this case
        )

        if is_individual_homogeneous_cluster_util:
            bar_legend_label = "Cluster Utilization"
        elif num_metrics_in_plot > 1:
            if "AvgCPUUtilization" == metric_definition["internal_key"]:
                bar_hatch = HATCH_CPU
            elif "AvgMemoryUtilization" == metric_definition["internal_key"]:
                bar_hatch = HATCH_MEMORY
            bar_legend_label = metric_definition["plot_title_metric_name"].replace("Average ",
                                                                                   "")  # Shorter label for legend
        else:
            bar_legend_label = metric_definition["plot_title_metric_name"]

        rects = ax.bar(bar_positions, current_metric_means, bar_width, label=bar_legend_label, color=bar_color,
                       hatch=bar_hatch,
                       yerr=current_metric_stds, capsize=5, alpha=ALPHA_VALUE, linewidth=LINE_WIDTH,
                       edgecolor=EDGE_COLOR)

        if num_metrics_in_plot > 1 or is_individual_homogeneous_cluster_util:  # Add to legend if multiple metrics OR it's the special individual case
            if not any(r.get_label() == bar_legend_label for r in legend_handles_for_plot):
                legend_handles_for_plot.append(rects)

        formatter = lambda val: f'{val:.1f}' if abs(val) > 1e-7 else ''
        ax.bar_label(rects, labels=[formatter(val) for val in current_metric_means], padding=3,
                     fontsize=max(8, FONT_SIZE - 2))

    if is_individual_homogeneous_cluster_util:
        ax.set_ylabel("Cluster Utilization [%]", fontsize=FONT_SIZE)
    elif num_metrics_in_plot > 1 and metrics_excel_keys == COMBINED_METRICS_PLOTS["CPU_Memory_Utilization"][
        "metrics_to_combine"]:
        ax.set_ylabel(COMBINED_METRICS_PLOTS["CPU_Memory_Utilization"]["plot_ylabel"], fontsize=FONT_SIZE)
    else:
        ax.set_ylabel(METRIC_CONFIG[metrics_excel_keys[0]]["plot_ylabel"], fontsize=FONT_SIZE)

    ax.set_xlabel("Configuration", fontsize=FONT_SIZE)
    ax.set_xticks(x_indices)
    ax.set_xticklabels([s.replace(", ", ",\n") for s in x_axis_configs_order], rotation=0, ha="center",
                       fontsize=max(10, FONT_SIZE))
    ax.tick_params(axis='y', which='major', labelsize=FONT_SIZE - 2)

    if legend_handles_for_plot:  # Check if list is not empty
        ax.legend(handles=legend_handles_for_plot, loc='lower center', bbox_to_anchor=(0.5, -0.24),
                  ncol=len(legend_handles_for_plot), fontsize=FONT_SIZE)

    max_y_val = (np.array(all_plot_means) + np.array(all_plot_stds)).max() if all_plot_means else 1
    ax.set_ylim(bottom=0, top=max(1, max_y_val * 1.15))
    ax.grid(axis='y', linestyle='--', alpha=0.7)


def plot_system_comparison(ax, all_systems_data, x_axis_configs_order, metrics_excel_keys,
                           current_variant_name_raw, unique_systems_to_plot, plot_file_name_suffix_arg):
    n_configs_on_x = len(x_axis_configs_order)
    x_indices = np.arange(n_configs_on_x)
    num_metrics_in_plot = len(metrics_excel_keys)
    num_systems = len(unique_systems_to_plot)
    total_width_for_config_group = 0.8
    bar_width_per_system = total_width_for_config_group / num_systems
    sub_bar_width = bar_width_per_system / num_metrics_in_plot if num_metrics_in_plot > 1 else bar_width_per_system

    legend_handles_for_plot = []
    all_plot_means = []
    all_plot_stds = []

    for sys_idx, system_name in enumerate(unique_systems_to_plot):
        system_color = SYSTEM_MAIN_COLORS.get(system_name, hex_to_rgba('grey', ALPHA_VALUE))
        system_group_offset = - (total_width_for_config_group / 2) + (sys_idx * bar_width_per_system) + (
                    bar_width_per_system / 2)

        for metric_idx, metric_key_excel in enumerate(metrics_excel_keys):
            metric_definition = METRIC_CONFIG[metric_key_excel]
            current_metric_means = pd.Series(index=x_axis_configs_order, dtype=float)
            current_metric_stds = pd.Series(index=x_axis_configs_order, dtype=float)
            data_for_current_system_metric = all_systems_data[
                (all_systems_data["System"] == system_name) & (all_systems_data["Metric"] == metric_key_excel)
                ]
            for config_name_on_x in x_axis_configs_order:
                row_data = data_for_current_system_metric[
                    data_for_current_system_metric["Configuration"] == config_name_on_x]
                if not row_data.empty and pd.notna(row_data["Average (calculated)"].iloc[0]):
                    current_metric_means[config_name_on_x] = row_data["Average (calculated)"].iloc[0] * \
                                                             metric_definition["conversion_factor"]
                    current_metric_stds[config_name_on_x] = row_data["Std. deviation (calculated)"].iloc[0] * \
                                                            metric_definition["conversion_factor"]

            current_metric_means = current_metric_means.fillna(0)
            current_metric_stds = current_metric_stds.fillna(0)
            all_plot_means.extend(current_metric_means.values)
            all_plot_stds.extend(current_metric_stds.values)

            bar_hatch = None
            if current_variant_name_raw == "homogeneous" and plot_file_name_suffix_arg == "ClusterUtilization":
                bar_label_for_legend = f"{system_name} - Cluster Util."
            elif num_metrics_in_plot == 1:
                bar_label_for_legend = system_name
            else:  # num_metrics_in_plot > 1
                if "AvgCPUUtilization" == metric_definition["internal_key"]:
                    bar_hatch = HATCH_CPU
                elif "AvgMemoryUtilization" == metric_definition["internal_key"]:
                    bar_hatch = HATCH_MEMORY
                bar_label_for_legend = f"{system_name} - {metric_definition['plot_title_metric_name'].replace('Average ', '')}"

            current_bar_width_to_plot = sub_bar_width
            if num_metrics_in_plot == 1:
                bar_positions = x_indices + system_group_offset
            else:
                metric_specific_offset = - (bar_width_per_system / 2) + (metric_idx * sub_bar_width) + (
                            sub_bar_width / 2)
                bar_positions = x_indices + system_group_offset + metric_specific_offset

            rects = ax.bar(bar_positions, current_metric_means, current_bar_width_to_plot, label=bar_label_for_legend,
                           color=system_color, hatch=bar_hatch,
                           yerr=current_metric_stds, capsize=3, alpha=ALPHA_VALUE, linewidth=LINE_WIDTH,
                           edgecolor=EDGE_COLOR)

            if not any(r.get_label() == bar_label_for_legend for r in legend_handles_for_plot):
                legend_handles_for_plot.append(rects)

            formatter = lambda val: f'{val:.1f}' if abs(val) > 1e-7 else ''
            ax.bar_label(rects, labels=[formatter(val) for val in current_metric_means], padding=2,
                         fontsize=max(6, FONT_SIZE - 4))

    if current_variant_name_raw == "homogeneous" and plot_file_name_suffix_arg == "ClusterUtilization":
        ax.set_ylabel("Cluster Utilization [%]", fontsize=FONT_SIZE)
    elif num_metrics_in_plot > 1 and metrics_excel_keys == COMBINED_METRICS_PLOTS["CPU_Memory_Utilization"][
        "metrics_to_combine"]:
        ax.set_ylabel(COMBINED_METRICS_PLOTS["CPU_Memory_Utilization"]["plot_ylabel"], fontsize=FONT_SIZE)
    else:  # Single metric or other combined
        ax.set_ylabel(METRIC_CONFIG[metrics_excel_keys[0]]["plot_ylabel"], fontsize=FONT_SIZE)

    ax.set_xlabel("Configuration", fontsize=FONT_SIZE)
    ax.set_xticks(x_indices)
    ax.set_xticklabels([s.replace(", ", ",\n") for s in x_axis_configs_order], rotation=0, ha="center",
                       fontsize=max(10, FONT_SIZE))
    ax.tick_params(axis='y', which='major', labelsize=FONT_SIZE - 2)
    if legend_handles_for_plot:
        legend_handles_for_plot.sort(key=lambda x: x.get_label())
        ax.legend(handles=legend_handles_for_plot, loc='lower center', bbox_to_anchor=(0.5, -0.30),
                  ncol=min(num_systems if num_metrics_in_plot == 1 else num_systems * num_metrics_in_plot, 3),
                  fontsize=FONT_SIZE - 2)
    max_y_val = (np.array(all_plot_means) + np.array(all_plot_stds)).max() if all_plot_means else 1
    ax.set_ylim(bottom=0, top=max(1, max_y_val * 1.20))
    ax.grid(axis='y', linestyle='--', alpha=0.7)


os.makedirs(OUTPUT_DIRECTORY, exist_ok=True)
unique_systems = sorted(data["System"].unique())

# --- Loop for individual system charts ---
for system_name in unique_systems:
    system_specific_data = data[data["System"] == system_name]
    for variant_key_lower in VARIANT_TYPES:
        variant_specific_data = system_specific_data[system_specific_data["Variant"] == variant_key_lower]
        if variant_specific_data.empty: continue
        current_x_axis_configs_ordered = CONFIG_ORDER.get(variant_key_lower,
                                                          sorted(variant_specific_data["Configuration"].unique()))
        actual_configs_in_data = sorted(variant_specific_data["Configuration"].unique())
        current_x_axis_configs = [cfg for cfg in current_x_axis_configs_ordered if cfg in actual_configs_in_data]
        if not current_x_axis_configs: continue

        for task in PLOT_TASKS:
            fig, ax = plt.subplots(figsize=(14, 8))
            plt.rcParams['hatch.linewidth'] = 1.0
            plot_file_name_base = f"{system_name.replace(' ', '_')}_{variant_key_lower}"

            metrics_for_plot = []
            plot_file_name_suffix = ""

            if task["type"] == "single":
                metric_excel_name = task["metric_excel_key"]
                if not any(variant_specific_data[(variant_specific_data["Configuration"] == conf) & (
                        variant_specific_data["Metric"] == metric_excel_name) & (
                                                 variant_specific_data["Average (calculated)"].notna())].empty for conf
                           in current_x_axis_configs):
                    metrics_for_plot = [metric_excel_name]
                    plot_file_name_suffix = METRIC_CONFIG[metric_excel_name]['internal_key']
                else:  # Check if any data exists at all for this metric
                    if variant_specific_data[(variant_specific_data["Metric"] == metric_excel_name) & (
                    variant_specific_data["Average (calculated)"].notna())].empty:
                        plt.close(fig)
                        continue


            elif task["type"] == "combined":
                combined_plot_details = COMBINED_METRICS_PLOTS[task["config_key"]]
                original_metrics = combined_plot_details["metrics_to_combine"]

                if variant_key_lower == "homogeneous" and task["config_key"] == "CPU_Memory_Utilization":
                    cpu_metric_key = next(
                        (m for m in original_metrics if METRIC_CONFIG[m]["internal_key"] == "AvgCPUUtilization"), None)
                    if cpu_metric_key:
                        metrics_for_plot = [cpu_metric_key]
                        plot_file_name_suffix = "ClusterUtilization"  # Distinguish from system comparison
                    else:  # Fallback
                        metrics_for_plot = [original_metrics[0]]
                        plot_file_name_suffix = combined_plot_details['internal_key'] + "_fallback_individual"
                else:
                    metrics_for_plot = original_metrics
                    plot_file_name_suffix = combined_plot_details['internal_key']

            if not metrics_for_plot:  # If no metrics selected (e.g. data missing for single, or bad config)
                plt.close(fig)
                continue

            # Final check if data truly exists for the selected metrics_for_plot
            plot_data_exists = False
            for conf in current_x_axis_configs:
                if not variant_specific_data[
                    (variant_specific_data["Configuration"] == conf) &
                    (variant_specific_data["Metric"].isin(metrics_for_plot)) &
                    (variant_specific_data["Average (calculated)"].notna())
                ].empty:
                    plot_data_exists = True
                    break
            if not plot_data_exists:
                plt.close(fig)
                continue

            plot_configuration_comparison(ax, variant_specific_data, current_x_axis_configs, metrics_for_plot,
                                          system_name, variant_key_lower, plot_file_name_suffix)  # Pass suffix

            plt.tight_layout(rect=[0, 0.05, 1, 0.93])  # Adjusted bottom margin slightly
            if len(metrics_for_plot) > 1 or plot_file_name_suffix == "ClusterUtilization":  # If legend is expected
                plt.subplots_adjust(bottom=0.20 if len(current_x_axis_configs) > 2 else 0.15)

            final_outfile_name = f"{plot_file_name_base}_{plot_file_name_suffix}.svg"
            plt.savefig(os.path.join(OUTPUT_DIRECTORY, final_outfile_name), format='svg', bbox_inches='tight')
            plt.close(fig)

# --- Loop for system comparison charts ---
if unique_systems:
    for variant_key_lower in VARIANT_TYPES:
        variant_specific_data_all_systems = data[data["Variant"] == variant_key_lower]
        if variant_specific_data_all_systems.empty: continue
        current_x_axis_configs_ordered = CONFIG_ORDER.get(variant_key_lower, sorted(
            variant_specific_data_all_systems["Configuration"].unique()))
        actual_configs_in_data = sorted(variant_specific_data_all_systems["Configuration"].unique())
        current_x_axis_configs_for_plot = [cfg for cfg in current_x_axis_configs_ordered if
                                           cfg in actual_configs_in_data]
        if not current_x_axis_configs_for_plot: continue

        for task in PLOT_TASKS:
            fig, ax = plt.subplots(figsize=(16, 9))
            plt.rcParams['hatch.linewidth'] = 1.0
            plot_file_name_base = f"SYSTEM_COMPARISON_{variant_key_lower}"
            metrics_for_current_plot_task = []
            plot_file_name_suffix = ""

            if task["type"] == "single":
                metric_excel_name = task["metric_excel_key"]
                metrics_for_current_plot_task = [metric_excel_name]
                plot_file_name_suffix = METRIC_CONFIG[metric_excel_name]['internal_key']
            elif task["type"] == "combined":
                combined_plot_details = COMBINED_METRICS_PLOTS[task["config_key"]]
                original_metrics = combined_plot_details["metrics_to_combine"]
                if variant_key_lower == "homogeneous" and task["config_key"] == "CPU_Memory_Utilization":
                    cpu_metric_key = next(
                        (m for m in original_metrics if METRIC_CONFIG[m]["internal_key"] == "AvgCPUUtilization"), None)
                    if cpu_metric_key:
                        metrics_for_current_plot_task = [cpu_metric_key]
                        plot_file_name_suffix = "ClusterUtilization"
                    else:
                        metrics_for_current_plot_task = [original_metrics[0]]
                        plot_file_name_suffix = combined_plot_details['internal_key'] + "_fallback"
                else:
                    metrics_for_current_plot_task = original_metrics
                    plot_file_name_suffix = combined_plot_details['internal_key']

            if not metrics_for_current_plot_task:
                plt.close(fig)
                continue

            data_exists = False
            for conf_name in current_x_axis_configs_for_plot:
                if not variant_specific_data_all_systems[
                    (variant_specific_data_all_systems["Configuration"] == conf_name) &
                    (variant_specific_data_all_systems["Metric"].isin(metrics_for_current_plot_task)) &
                    (variant_specific_data_all_systems["Average (calculated)"].notna())
                ].empty:
                    data_exists = True
                    break
            if not data_exists:
                plt.close(fig)
                continue

            plot_system_comparison(ax, variant_specific_data_all_systems, current_x_axis_configs_for_plot,
                                   metrics_for_current_plot_task, variant_key_lower, unique_systems,
                                   plot_file_name_suffix)

            plt.tight_layout(rect=[0, 0.05, 1, 0.93])
            if len(unique_systems) * len(
                    metrics_for_current_plot_task) > 3 or plot_file_name_suffix == "ClusterUtilization":
                plt.subplots_adjust(bottom=0.25 if len(current_x_axis_configs_for_plot) > 2 else 0.20)

            final_outfile_name = f"{plot_file_name_base}_{plot_file_name_suffix}.svg"
            plt.savefig(os.path.join(OUTPUT_DIRECTORY, final_outfile_name), format='svg', bbox_inches='tight')
            plt.close(fig)

print(f"\nAll chart generation complete. Output directory: {OUTPUT_DIRECTORY}")
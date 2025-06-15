#!/usr/bin/env python3
import os
import matplotlib as mpl
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

# --- Configuration: Names, Mappings, Colors ---
FONT_SIZE = 16

# Target English names for cluster sizes on the X-axis
TARGET_CLUSTER_SIZE_ORDER = ["Small", "Medium", "Big"]

# Mapping from cluster size names in Excel to target English names
CLUSTER_SIZE_MAP_FROM_EXCEL = {
    "small cluster": "Small",
    "medium cluster": "Medium",
    "big cluster": "Big",
}

# Mapping from variant names in Excel to internal keys
VARIANT_MAP_FROM_EXCEL = {
    "default config": "default_config",
    "multiple queues": "multiple_queues",
}

# Metric configurations:
# Keys are metric names AS THEY APPEAR IN THE EXCEL 'Metric' COLUMN
METRIC_CONFIG = {
    "Turnaround time [s]": {
        "internal_key": "TurnaroundTime",
        "plot_ylabel": "Time [s]", # Y-axis label is seconds
        "conversion_factor": 1.0,  # Keep in seconds
        "plot_title_metric_name": "Average Turnaround Time"
    },
    "Cluster utilization [%]": {
        "internal_key": "ClusterUtilization",
        "plot_ylabel": "Cluster Utilization [%]",
        "conversion_factor": 1.0,  # No conversion
        "plot_title_metric_name": "Average Cluster Utilization"
    }
}

# System name for baseline in data
BASELINE_SYSTEM_NAME_IN_DATA = "vanilla k8s"
BASELINE_VARIANT_NAME_IN_DATA = "default_config"
BASELINE_LEGEND_LABEL = "Kube-scheduler (baseline)"

# Colors for the plots (using RGBA for alpha/transparency)
def hex_to_rgba(hex_color, alpha):
    rgb = mpl.colors.hex2color(hex_color)
    return (*rgb, alpha)

# Define base colors and alpha for bars
ALPHA_VALUE = 1 # Opacity value (0=transparent, 1=opaque)
EDGE_COLOR = 'black'
LINE_WIDTH = 1.0

COLOR_BASELINE_HEX = 'grey'
SYSTEM_MAIN_COLORS_HEX = {
    'Kueue': 'blue',
    'Volcano': 'red',
    'Yunikorn': 'green'
}
COLOR_MULTIPLE_QUEUES_PRIMARY_HEX = 'darkorange'

# Convert hex colors to RGBA
COLOR_BASELINE = hex_to_rgba(COLOR_BASELINE_HEX, ALPHA_VALUE)
SYSTEM_MAIN_COLORS = {k: hex_to_rgba(v, ALPHA_VALUE) for k, v in SYSTEM_MAIN_COLORS_HEX.items()}
COLOR_MULTIPLE_QUEUES_PRIMARY = hex_to_rgba(COLOR_MULTIPLE_QUEUES_PRIMARY_HEX, ALPHA_VALUE)

# SYSTEM_MAIN_COLORS['Kueue'] = (0.498, 0.498, 1.0, 1.0)
# SYSTEM_MAIN_COLORS['Volcano'] = (1, 0.498, 0.498, 1.0)
# SYSTEM_MAIN_COLORS['Yunikorn'] = (0.498, 0.745, 0.498, 1.0)
COLOR_BASELINE = (0.745, 0.745, 0.745, 1)

OUTPUT_DIRECTORY = "results_backfill"

# --- Data Loading and Preparation ---
try:
    raw_df = pd.read_excel("Wyniki.xlsx", sheet_name="Backfill")
except FileNotFoundError:
    print("Error: File Wyniki.xlsx not found. Ensure it is in the same directory as the script.")
    exit()
except ValueError as e:
    if "Sheet_name" in str(e):
        print("Error: Sheet 'Backfill' not found in Wyniki.xlsx. Please check the sheet name.")
        exit()
    raise e

print("Raw data head:\n", raw_df.head().to_string())

blocks = []
# Exact English headers expected in the Excel file
english_headers = [
    "Variant",
    "Cluster size",
    "System",
    "Metric",
    "Average (calculated)",
    "Std. deviation (calculated)"
]

for suffix in ["", ".1", ".2", ".3"]:
    current_excel_cols = [f"{col}{suffix}" for col in english_headers]
    # Check if the *first* column with the suffix exists as a proxy for the block
    if not current_excel_cols[0] in raw_df.columns:
        continue

    # Check if *all* columns for this suffix exist
    missing_cols = [col for col in current_excel_cols if col not in raw_df.columns]
    if missing_cols:
        print(f"Warning: Skipping column set with suffix '{suffix}'. Missing columns: {', '.join(missing_cols)}")
        continue

    print(f"Using columns for suffix '{suffix}': {current_excel_cols}")
    blk = raw_df[current_excel_cols].copy()
    # Rename columns to the base English headers (removing suffixes)
    blk.columns = english_headers
    blocks.append(blk)


if not blocks:
    print("Error: No data blocks loaded. Check column names in 'Wyniki.xlsx' (sheet 'Backfill').")
    print(f"Expected exact English headers (potentially with .1, .2 suffixes): {english_headers}")
    exit()

data = pd.concat(blocks, ignore_index=True)

print("\nConcatenated Data Head:\n", data.head().to_string())

# Clean data
data = data.dropna(subset=["Variant", "Metric", "System", "Cluster size"])
# Remove potential header rows repeated in the data
for header_name in english_headers:
     data = data[data[header_name] != header_name]


print("\nData after initial cleaning:\n", data.head().to_string())

# Convert numeric values
data["Average (calculated)"] = pd.to_numeric(data["Average (calculated)"], errors="coerce")
data["Std. deviation (calculated)"] = pd.to_numeric(data["Std. deviation (calculated)"], errors="coerce")

# Apply mappings after cleaning potential header rows
data["Variant"] = data["Variant"].str.lower().map(VARIANT_MAP_FROM_EXCEL).fillna(data["Variant"])
data["Cluster size"] = data["Cluster size"].str.lower().map(CLUSTER_SIZE_MAP_FROM_EXCEL).fillna(data["Cluster size"])


# Drop rows where essential numeric data couldn't be converted
data = data.dropna(subset=["Average (calculated)"])
# Fill missing std deviations with 0 AFTER converting to numeric
data["Std. deviation (calculated)"] = data["Std. deviation (calculated)"].fillna(0)


print("\nFinal Processed Data Head:\n", data.head().to_string())

# --- Plotting Function ---
def plot_comparison_chart(ax, x_labels, mean_data_dict, std_dev_dict, current_system_name, metric_config_entry):
    n_labels = len(x_labels)
    x = np.arange(n_labels)
    bar_width = 0.25

    # Helper function to get data or zeros series, aligned to x_labels
    def get_data(data_dict, key, index):
        series = data_dict.get(key, pd.Series(dtype=float))
        return series.reindex(index).fillna(0)

    # 1. Baseline (Kube-scheduler)
    baseline_means = get_data(mean_data_dict, "baseline", x_labels)
    baseline_stds = get_data(std_dev_dict, "baseline", x_labels)
    # Add error bars (yerr), transparency (alpha), and edge color
    rects1 = ax.bar(x - bar_width, baseline_means, bar_width,
                    label=BASELINE_LEGEND_LABEL, color=COLOR_BASELINE,
                    yerr=baseline_stds, capsize=5,
                    alpha=ALPHA_VALUE, linewidth=LINE_WIDTH, edgecolor=EDGE_COLOR)

    # 2. System with default config
    default_means = get_data(mean_data_dict, "default_config", x_labels)
    default_stds = get_data(std_dev_dict, "default_config", x_labels)
    system_default_color = SYSTEM_MAIN_COLORS.get(current_system_name, hex_to_rgba('purple', ALPHA_VALUE)) # Fallback RGBA color
    # Add error bars (yerr), transparency (alpha), and edge color
    rects2 = ax.bar(x, default_means, bar_width,
                    label=f"{current_system_name} with default config", color=system_default_color,
                    yerr=default_stds, capsize=5,
                    alpha=ALPHA_VALUE, linewidth=LINE_WIDTH, edgecolor=EDGE_COLOR)

    # 3. System with multiple queues
    multiple_means = get_data(mean_data_dict, "multiple_queues", x_labels)
    multiple_stds = get_data(std_dev_dict, "multiple_queues", x_labels)
    color_mq = COLOR_MULTIPLE_QUEUES_PRIMARY
    # Add error bars (yerr), transparency (alpha), and edge color
    rects3 = ax.bar(x + bar_width, multiple_means, bar_width,
                    label=f"{current_system_name} with multiple queues", color=color_mq,
                    yerr=multiple_stds, capsize=5,
                    alpha=ALPHA_VALUE, linewidth=LINE_WIDTH, edgecolor=EDGE_COLOR)

    # Use a formatter to hide labels for zero values
    formatter = lambda val: f'{val:.2f}' if abs(val) > 1e-9 else ''

    for rects_group in [rects1, rects2, rects3]:
         labels = [formatter(val) for val in rects_group.datavalues]
         ax.bar_label(rects_group, labels=labels, padding=3, fontsize=FONT_SIZE)


    ax.set_ylabel(metric_config_entry["plot_ylabel"], fontsize=FONT_SIZE)
    # ax.set_title(f"{metric_config_entry['plot_title_metric_name']} Comparison: {current_system_name}", fontsize=FONT_SIZE)
    ax.set_xticks(x)
    ax.set_xticklabels(x_labels)
    ax.set_xlabel("Cluster size", fontsize=FONT_SIZE)
    ax.tick_params(axis='both', which='major', labelsize=FONT_SIZE)
    ax.legend(
        loc='lower center',  # Anchor point is the bottom-center of the legend box
        bbox_to_anchor=(0.5, -0.24),  # Place that anchor point at x=0.5 (center), y=1.02 (just above the axes)
        ncol=3,  # Arrange legend items horizontally (adjust if too wide)
        frameon=True,  # Optional: Remove the frame around the legend
        fontsize=FONT_SIZE  # Optional: Adjust font size if needed
    )

    # Adjust Y limits considering error bars
    max_val_on_plot = 0
    for means, stds in [(baseline_means, baseline_stds), (default_means, default_stds), (multiple_means, multiple_stds)]:
        if not means.empty:
            upper_bound = (means + stds).max()
            if pd.notna(upper_bound):
                max_val_on_plot = max(max_val_on_plot, upper_bound)

    min_val_on_plot = 0
    for means, stds in [(baseline_means, baseline_stds), (default_means, default_stds), (multiple_means, multiple_stds)]:
        if not means.empty:
            lower_bound = (means - stds).min()
            if pd.notna(lower_bound):
                min_val_on_plot = min(min_val_on_plot, lower_bound)

    ax.set_ylim(bottom=min(0, min_val_on_plot * 1.1) if min_val_on_plot < 0 else 0 ,
                top=max(1, max_val_on_plot * 1.15)) # Ensure top is at least 1


# --- NEW FUNCTION: Plot Scheduler Comparison for a specific variant and metric ---
def plot_scheduler_comparison(data, variant_name, metric_name, metric_config):
    """
    Create a comparison chart showing all schedulers (baseline + 3 systems) for a specific variant and metric
    Similar to the images provided in the request.

    Parameters:
    - data: DataFrame with the data
    - variant_name: Name of the variant to plot (e.g., "default_config")
    - metric_name: Name of the metric as it appears in Excel
    - metric_config: Configuration for the metric from METRIC_CONFIG
    """
    print(f"\nGenerating scheduler comparison chart for Variant: {variant_name}, Metric: {metric_name}")

    # Filter data for the specific variant and metric
    filtered_data = data[
        (data["Variant"] == variant_name) &
        (data["Metric"] == metric_name)
        ].copy()

    # Always add baseline data (with default_config) regardless of current variant
    baseline_data = data[
        (data["System"].str.lower() == BASELINE_SYSTEM_NAME_IN_DATA.lower()) &
        (data["Variant"] == "default_config") &  # Always use default_config for baseline
        (data["Metric"] == metric_name)
        ].copy()

    # Combine baseline and filtered data
    combined_data = pd.concat([baseline_data, filtered_data])

    if combined_data.empty:
        print(f"  No data found for Variant: {variant_name}, Metric: {metric_name}")
        return

    # Identify all systems in this dataset
    systems = []
    # Always include baseline first
    if not baseline_data.empty:
        systems.append(BASELINE_SYSTEM_NAME_IN_DATA)

    # Add other systems
    other_systems = [
        system for system in filtered_data["System"].unique()
        if system.lower() != BASELINE_SYSTEM_NAME_IN_DATA.lower()
    ]
    systems.extend(sorted(other_systems))

    # Define bar positions and width
    n_cluster_sizes = len(TARGET_CLUSTER_SIZE_ORDER)
    n_systems = len(systems)
    bar_width = 0.8 / n_systems  # Divide available width (0.8) by number of systems

    # Create figure
    fig, ax = plt.subplots(figsize=(13, 7))
    x = np.arange(n_cluster_sizes)

    # Store bar handles for legend
    bar_handles = []

    for i, system in enumerate(systems):
        # Get data for this system, use appropriate variant
        if system.lower() == BASELINE_SYSTEM_NAME_IN_DATA.lower():
            # Always use default_config for baseline
            system_data = baseline_data
        else:
            # Use current variant for other systems
            system_data = filtered_data[filtered_data["System"].str.lower() == system.lower()]

        # Group by cluster size and calculate mean
        means = system_data.groupby("Cluster size")["Average (calculated)"].mean() * metric_config["conversion_factor"]
        stds = system_data.groupby("Cluster size")["Std. deviation (calculated)"].mean() * metric_config[
            "conversion_factor"]

        # Align with target cluster sizes
        aligned_means = means.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)
        aligned_stds = stds.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)

        # Calculate bar positions
        bar_positions = x - 0.4 + (i + 0.5) * bar_width

        # Determine color for system
        if system.lower() == BASELINE_SYSTEM_NAME_IN_DATA.lower():
            color = COLOR_BASELINE
            system_label = BASELINE_LEGEND_LABEL  # Use baseline label without "(default)"
        else:
            color = SYSTEM_MAIN_COLORS.get(system, hex_to_rgba('purple', ALPHA_VALUE))
            system_label = system  # Just the system name without "(default)"

        # Plot bars
        bars = ax.bar(
            bar_positions,
            aligned_means,
            width=bar_width,
            color=color,
            linewidth=LINE_WIDTH,
            yerr=aligned_stds,
            label=system_label,
            capsize=5,
            edgecolor=EDGE_COLOR
        )

        for j, value in enumerate(aligned_means):
            if value > 0:
                label_y_position = value + aligned_stds[
                    j] + 0.5
                ax.text(
                    bar_positions[j],
                    label_y_position,
                    f"{value:.1f}",
                    ha='center',
                    va='bottom',
                    fontsize=FONT_SIZE
                )

        bar_handles.append(bars)

    # Configure the chart
    ax.set_ylabel(metric_config["plot_ylabel"], fontsize=FONT_SIZE)
    variant_title = "default" if variant_name == "default_config" else "multiple queues"
    # ax.set_title(
    #     f"{metric_config['plot_title_metric_name']} comparison across schedulers with {variant_title} configurations", fontsize=FONT_SIZE)
    ax.set_xticks(x)
    ax.set_xticklabels(TARGET_CLUSTER_SIZE_ORDER)
    ax.set_xlabel("Cluster size", fontsize=FONT_SIZE)
    ax.tick_params(axis='both', which='major', labelsize=FONT_SIZE)
    ax.legend(
        loc='lower center',  # Anchor point is the bottom-center of the legend box
        bbox_to_anchor=(0.5, -0.24),  # Place that anchor point at x=0.5 (center), y=1.02 (just above the axes)
        ncol=n_systems,  # Arrange legend items horizontally (adjust if too wide)
        frameon=True,  # Optional: Remove the frame around the legend
        fontsize=FONT_SIZE  # Optional: Adjust font size if needed
    )

    # Adjust y-limits
    all_values = combined_data["Average (calculated)"] * metric_config["conversion_factor"]
    max_value = all_values.max() * 1.15  # Add some padding
    ax.set_ylim(0, max(1, max_value))

    # Define human-readable name for the file
    variant_display = variant_name.replace("_", "-")
    safe_metric_name = metric_config["internal_key"]
    filename = f"scheduler_comparison_{variant_display}_{safe_metric_name}.svg"

    # Save figure
    plt.tight_layout()
    plt.savefig(os.path.join(OUTPUT_DIRECTORY, filename), format='svg')
    print(f"  Chart saved: {os.path.join(OUTPUT_DIRECTORY, filename)}")
    plt.close(fig)

# --- Main Plotting Loop ---
os.makedirs(OUTPUT_DIRECTORY, exist_ok=True)

baseline_data_for_plots = data[
    (data["System"].str.lower() == BASELINE_SYSTEM_NAME_IN_DATA.lower()) &
    (data["Variant"] == BASELINE_VARIANT_NAME_IN_DATA)
].copy()

# Exclude baseline system name case-insensitively
systems_to_compare = sorted([s for s in data["System"].unique() if s.lower() != BASELINE_SYSTEM_NAME_IN_DATA.lower()])


if not systems_to_compare:
    print(f"Warning: No systems found to compare (other than '{BASELINE_SYSTEM_NAME_IN_DATA}'). Check 'System' column data.")

for system_name in systems_to_compare:
    print(f"\nProcessing system: {system_name}")
    for metric_excel_name_key, metric_entry in METRIC_CONFIG.items():
        print(f"  Metric: {metric_excel_name_key}")
        plot_mean_data_series = {}
        plot_std_dev_series = {} # Store aggregated standard deviations

        # --- Aggregate Data for Plotting ---
        def aggregate_data(df, metric_key, conversion):
            filtered = df[df["Metric"] == metric_key]
            if filtered.empty:
                return pd.Series(dtype=float), pd.Series(dtype=float)

            # Group by Cluster size and calculate mean and std dev
            agg = filtered.groupby("Cluster size").agg(
                Mean=('Average (calculated)', 'mean'),
                # Use mean of std dev if multiple runs per cluster size exist
                StdDev=('Std. deviation (calculated)', 'mean')
            )

            means = agg['Mean'] * conversion
            # Apply conversion factor to std dev as well
            std_devs = agg['StdDev'] * conversion

            return means, std_devs

        # 1. Baseline Data
        baseline_means, baseline_stds = aggregate_data(baseline_data_for_plots, metric_excel_name_key, metric_entry["conversion_factor"])
        if not baseline_means.empty:
            plot_mean_data_series["baseline"] = baseline_means
            plot_std_dev_series["baseline"] = baseline_stds
            # print(f"    Baseline Means:\n{baseline_means.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")
            # print(f"    Baseline Stds:\n{baseline_stds.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")


        # 2. System Data (default config)
        system_default_data = data[
            (data["System"] == system_name) &
            (data["Variant"] == "default_config")
        ]
        default_means, default_stds = aggregate_data(system_default_data, metric_excel_name_key, metric_entry["conversion_factor"])
        if not default_means.empty:
            plot_mean_data_series["default_config"] = default_means
            plot_std_dev_series["default_config"] = default_stds
            # print(f"    Default Means:\n{default_means.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")
            # print(f"    Default Stds:\n{default_stds.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")

        # 3. System Data (multiple queues)
        system_multiple_data = data[
            (data["System"] == system_name) &
            (data["Variant"] == "multiple_queues")
        ]
        multiple_means, multiple_stds = aggregate_data(system_multiple_data, metric_excel_name_key, metric_entry["conversion_factor"])
        if not multiple_means.empty:
            plot_mean_data_series["multiple_queues"] = multiple_means
            plot_std_dev_series["multiple_queues"] = multiple_stds
            # print(f"    Multiple Queues Means:\n{multiple_means.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")
            # print(f"    Multiple Queues Stds:\n{multiple_stds.reindex(TARGET_CLUSTER_SIZE_ORDER).fillna(0)}")


        # Check if there's any data to plot for this metric and system
        has_data_for_plot = any(
            not series.empty for series in plot_mean_data_series.values() if isinstance(series, pd.Series)
        )

        if has_data_for_plot:
            fig, ax = plt.subplots(figsize=(13, 7))
            # Pass std dev data to plotting function
            plot_comparison_chart(ax, TARGET_CLUSTER_SIZE_ORDER,
                                  plot_mean_data_series, plot_std_dev_series, # Pass both dicts
                                  system_name, metric_entry)

            plt.tight_layout()

            safe_system_name = system_name.replace(" ", "_").replace("/", "_")
            outfile_name = f"{safe_system_name}_{metric_entry['internal_key']}_comparison.svg"
            plt.savefig(os.path.join(OUTPUT_DIRECTORY, outfile_name), format='svg')
            print(f"    Chart saved: {os.path.join(OUTPUT_DIRECTORY, outfile_name)}")
            plt.close(fig)
        else:
            print(f"    Skipping chart: No data found for System: {system_name}, Metric: {metric_excel_name_key}.")

# --- Generate Scheduler Comparison Charts (Like the Provided Images) ---
print("\n=== Generating Scheduler Comparison Charts ===")

# Generate for each variant and metric combination
for variant_name in ["default_config", "multiple_queues"]:
    for metric_excel_name_key, metric_entry in METRIC_CONFIG.items():
        plot_scheduler_comparison(data, variant_name, metric_excel_name_key, metric_entry)


print(f"\nComparison charts generation complete. Output directory: {OUTPUT_DIRECTORY}")
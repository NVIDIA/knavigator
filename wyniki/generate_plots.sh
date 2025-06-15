#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'

# Function to run a single script and check its exit code
run_script() {
    local script_name="$1"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting: $script_name"
    if ! python3.13 "$script_name"; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Error running $script_name" >&2
        exit 1
    fi
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Finished: $script_name"
    echo
}

# Main entry point
main() {
    # List of scripts to run (adjust paths if needed)
    local scripts=(
        "wyniki/wyniki_swiadomosc_topologii.py"
        "wyniki/wyniki_wydajnosc_skalowalnosc.py"
        "wyniki/wyniki_sprawiedliwosc.py"
        "wyniki/wyniki_wydajnosc_skalowalnosc_enhanced.py"
    )

    for script in "${scripts[@]}"; do
        if [[ ! -f "$script" ]]; then
            echo "File not found: $script" >&2
            exit 1
        fi
        run_script "$script"
    done

    echo "All scripts completed successfully."
}

main "$@"

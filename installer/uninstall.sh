#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Uninstaller Script
#  Clean Teardown & Service Removal
#  GitHub: https://github.com/F-e-n-y-x/NivaroOS
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -euo pipefail

SRC_DIR="/opt/nivaroos/src"
ALL_UNITS="nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service nivaroos-vm-sidecar.service rclone.service usb-mount@.service"
MANIFEST_FILE="/var/lib/nivaroos/manifest"

PURGE_DATA=""
YES=""
STEP_NUM=0
TOTAL_STEPS=4
START_TIME=0

# ------------------------------------------------------------------------------
# Terminal & Color Formatting
# ------------------------------------------------------------------------------
IS_TTY="false"
if [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && [ -z "${NO_COLOR:-}" ]; then
	IS_TTY="true"
fi

if [ "$IS_TTY" = "true" ]; then
	COLOR_RESET='\033[0m'
	COLOR_BOLD='\033[1m'
	COLOR_DIM='\033[2m'
	COLOR_CYAN='\033[38;5;51m'
	COLOR_BLUE='\033[38;5;39m'
	COLOR_GREEN='\033[38;5;48m'
	COLOR_YELLOW='\033[38;5;220m'
	COLOR_RED='\033[38;5;196m'
	COLOR_PURPLE='\033[38;5;141m'
	COLOR_MUTED='\033[38;5;244m'
	COLOR_WHITE='\033[38;5;255m'
else
	COLOR_RESET=''
	COLOR_BOLD=''
	COLOR_DIM=''
	COLOR_CYAN=''
	COLOR_BLUE=''
	COLOR_GREEN=''
	COLOR_YELLOW=''
	COLOR_RED=''
	COLOR_PURPLE=''
	COLOR_MUTED=''
	COLOR_WHITE=''
fi

SPINNER_FRAMES=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")

info()    { printf '%b\n' "${COLOR_CYAN}ℹ${COLOR_RESET}  ${COLOR_WHITE}$1${COLOR_RESET}"; }
success() { printf '%b\n' "${COLOR_GREEN}✔${COLOR_RESET}  ${COLOR_GREEN}$1${COLOR_RESET}"; }
warn()    { printf '%b\n' "${COLOR_YELLOW}⚠${COLOR_RESET}  ${COLOR_YELLOW}$1${COLOR_RESET}" >&2; }
error()   { printf '%b\n' "${COLOR_RED}✖${COLOR_RESET}  ${COLOR_RED}$1${COLOR_RESET}" >&2; }

cleanup_on_exit() {
	if [ "$IS_TTY" = "true" ]; then
		printf "\033[?25h"
	fi
}
trap cleanup_on_exit EXIT INT TERM

get_terminal_width() {
	local c=80
	if command -v tput >/dev/null 2>&1; then
		c=$(tput cols 2>/dev/null || echo "${COLUMNS:-80}")
	elif [ -n "${COLUMNS:-}" ]; then
		c="$COLUMNS"
	fi
	if [ "$c" -lt 40 ]; then c=80; fi
	echo "$c"
}

get_terminal_height() {
	local l=24
	if command -v tput >/dev/null 2>&1; then
		l=$(tput lines 2>/dev/null || echo "${LINES:-24}")
	elif [ -n "${LINES:-}" ]; then
		l="$LINES"
	fi
	if [ "$l" -lt 10 ]; then l=24; fi
	echo "$l"
}

check_root() {
	if [ "$(id -u)" -ne 0 ]; then
		if command -v sudo >/dev/null 2>&1; then
			info "Root privileges required. Elevating with sudo..."
			exec sudo -E bash "$0" "$@"
		else
			error "NivaroOS uninstaller must be run as root."
			exit 1
		fi
	fi
}

print_banner() {
	if [ "$IS_TTY" = "true" ]; then
		clear 2>/dev/null || true
	fi
	printf "\n"
	printf '%b' "${COLOR_BOLD}${COLOR_CYAN}"
	cat <<'EOF'
    _   _ _                         ___  ____  
   | \ | (_)_   ____ _ _ __ ___    / _ \/ ___| 
   |  \| | \ \ / / _` | '__/ _ \  | | | \___ \ 
   | |\  | |\ V / (_| | | | (_) | | |_| |___) |
   |_| \_|_| \_/ \__,_|_|  \___/   \___/|____/ 
EOF
	printf '%b\n' "${COLOR_RESET}"
	printf '%b\n\n' "   ${COLOR_PURPLE}✦${COLOR_RESET} ${COLOR_BOLD}NivaroOS Complete Teardown & Uninstaller${COLOR_RESET} ${COLOR_PURPLE}✦${COLOR_RESET}"
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--purge-data) PURGE_DATA=yes ;;
			--yes|-y|--unattended) YES=yes ;;
			--help|-h)
				print_banner
				printf '%b\n' "${COLOR_BOLD}Usage:${COLOR_RESET} uninstall.sh [options]\n"
				printf '%b\n' "${COLOR_BOLD}Options:${COLOR_RESET}"
				printf '%b\n' "  ${COLOR_CYAN}-y, --yes${COLOR_RESET}             Automatic non-interactive uninstall (skip confirmation)"
				printf '%b\n' "  ${COLOR_CYAN}--purge-data${COLOR_RESET}          Permanently delete /DATA (all app configs, VM disks, files)"
				printf '%b\n' "  ${COLOR_CYAN}-h, --help${COLOR_RESET}            Display this help message and exit"
				printf '\n'
				exit 0
				;;
			*)
				error "Unknown argument '$1'. Run with --help to see available options."
				exit 1
				;;
		esac
		shift
	done
}

confirm_uninstall() {
	if [ -n "$YES" ]; then
		return
	fi

	if [ ! -t 0 ]; then
		return
	fi

	printf '%b\n' "${COLOR_BOLD}${COLOR_WHITE}You are about to uninstall NivaroOS from this machine.${COLOR_RESET}"
	printf '%b\n' "  • All NivaroOS systemd background services will be stopped and removed."
	printf '%b\n' "  • System binaries (/usr/bin/nivaroos*) and web assets will be deleted."
	if [ "$PURGE_DATA" = "yes" ]; then
		printf '%b\n' "  ${COLOR_RED}• WARNING: /DATA will be permanently erased (all app databases, VM disks, files).${COLOR_RESET}"
	else
		printf '%b\n' "  • User data in ${COLOR_GREEN}/DATA${COLOR_RESET} will be ${COLOR_GREEN}PRESERVED${COLOR_RESET} safely."
	fi
	printf '\n'

	local reply=""
	printf '%b' "  ${COLOR_CYAN}?${COLOR_RESET} ${COLOR_BOLD}Proceed with uninstallation?${COLOR_RESET} [y/N]: "
	read -r reply </dev/tty || reply=""
	case "$reply" in
		[yY]|[yY][eE][sS]) ;;
		*)
			info "Uninstallation aborted."
			exit 0
			;;
	esac
	printf '\n'
}

run_step() {
	local title="$1"
	shift
	STEP_NUM=$((STEP_NUM + 1))
	local step_tag="[${STEP_NUM}/${TOTAL_STEPS}]"
	local start_ts
	start_ts=$(date +%s)

	local log_file
	log_file=$(mktemp /tmp/nivaroos-uninstall-step-XXXXXX.log)

	local cols lines_cnt
	cols=$(get_terminal_width)
	lines_cnt=$(get_terminal_height)

	local box_width=$((cols - 4))
	if [ "$box_width" -lt 40 ]; then box_width=40; fi
	local inner_width=$((box_width - 4))

	local num_log_lines=5
	if [ "$lines_cnt" -ge 35 ]; then
		num_log_lines=7
	elif [ "$lines_cnt" -le 20 ]; then
		num_log_lines=3
	fi

	local total_rendered_lines=$((num_log_lines + 3))

	if [ "$IS_TTY" = "true" ]; then
		(
			eval "$*"
		) > "$log_file" 2>&1 </dev/null &
		local cmd_pid=$!

		local frame_idx=0
		local num_frames=${#SPINNER_FRAMES[@]}
		local first_render=true

		printf "\033[?25l"

		while kill -0 "$cmd_pid" 2>/dev/null; do
			local current_ts
			current_ts=$(date +%s)
			local elapsed=$((current_ts - start_ts))
			local frame="${SPINNER_FRAMES[$frame_idx]}"

			if [ "$first_render" = "false" ]; then
				printf "\033[%dA" "$total_rendered_lines"
			else
				first_render=false
			fi

			printf "\r\033[2K  %b %b %b %b(%ds)%b\n" \
				"${COLOR_CYAN}${frame}${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_MUTED}" "${elapsed}" "${COLOR_RESET}"

			local title_tag="Teardown Activity"
			local top_dashes_len=$((box_width - ${#title_tag} - 6))
			if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
			local top_dashes=""
			for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

			printf "\r\033[2K  %b╭── %b%s%b %s╮%b\n" \
				"${COLOR_MUTED}" "${COLOR_CYAN}" "${title_tag}" "${COLOR_MUTED}" "${top_dashes}" "${COLOR_RESET}"

			local lines=()
			if [ -f "$log_file" ] && [ -s "$log_file" ]; then
				mapfile -t lines < <(tail -n "$num_log_lines" "$log_file" 2>/dev/null || true)
			fi

			local pad_count=$((num_log_lines - ${#lines[@]}))
			for ((p=0; p<pad_count; p++)); do
				printf "\r\033[2K  %b│%b  %-*s  %b│%b\n" \
					"${COLOR_MUTED}" "${COLOR_MUTED}" "$inner_width" "..." "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			for l in "${lines[@]}"; do
				local clean_l
				clean_l=$(printf '%s' "$l" | tr '\r\t' '  ' | cut -c 1-"$inner_width")
				printf "\r\033[2K  %b│%b  %-*s  %b│%b\n" \
					"${COLOR_MUTED}" "${COLOR_WHITE}" "$inner_width" "$clean_l" "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			local bot_dashes=""
			for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done
			printf "\r\033[2K  %b╰%s╯%b\n" "${COLOR_MUTED}" "${bot_dashes}" "${COLOR_RESET}"

			frame_idx=$(( (frame_idx + 1) % num_frames ))
			sleep 0.08
		done

		wait "$cmd_pid"
		local exit_code=$?
		local end_ts
		end_ts=$(date +%s)
		local total_elapsed=$((end_ts - start_ts))

		if [ "$first_render" = "false" ]; then
			printf "\033[%dA" "$total_rendered_lines"
			for ((c=0; c<total_rendered_lines; c++)); do
				printf "\r\033[2K\n"
			done
			printf "\033[%dA" "$total_rendered_lines"
		fi

		printf "\033[?25h"

		if [ "$exit_code" -eq 0 ]; then
			printf "\r\033[2K  %b %b %b %b[%ds]%b\n" \
				"${COLOR_GREEN}✔${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_MUTED}" "${total_elapsed}" "${COLOR_RESET}"
			rm -f "$log_file"
		else
			printf "\r\033[2K  %b %b %b %b[%ds - FAILED]%b\n" \
				"${COLOR_RED}✖${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_RED}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_RED}" "${total_elapsed}" "${COLOR_RESET}"
			rm -f "$log_file"
		fi
	else
		printf "  ➜ %s %s...\n" "$step_tag" "$title"
		if eval "$*" > "$log_file" 2>&1 </dev/null; then
			local end_ts
			end_ts=$(date +%s)
			local total_elapsed=$((end_ts - start_ts))
			printf "  ✔ %s %s [%ds]\n" "$step_tag" "$title" "$total_elapsed"
			rm -f "$log_file"
		else
			local exit_code=$?
			printf "  ✖ %s %s [FAILED with exit code %d]\n" "$step_tag" "$title" "$exit_code"
			rm -f "$log_file"
		fi
	fi
}

stop_services() {
	run_step "Stopping active systemd services & background daemons" "
		for unit in $ALL_UNITS; do
			systemctl disable --now \"\$unit\" >/dev/null 2>&1 || true
		done
	"
}

remove_unit_files() {
	run_step "Removing systemd service definitions & reloading daemon" "
		if [ -f \"$MANIFEST_FILE\" ]; then
			while IFS= read -r f; do
				case \"\$f\" in
					*.service)
						rm -f \"\$f\" 2>/dev/null || true
						;;
				esac
			done < \"$MANIFEST_FILE\"
		fi
		rm -f \
			/usr/lib/systemd/system/nivaroos-gateway.service \
			/usr/lib/systemd/system/nivaroos-gateway.service.buildroot \
			/usr/lib/systemd/system/nivaroos-message-bus.service \
			/usr/lib/systemd/system/nivaroos.service \
			/usr/lib/systemd/system/nivaroos-user-service.service \
			/usr/lib/systemd/system/nivaroos-app-management.service \
			/usr/lib/systemd/system/nivaroos-app-management.service.buildroot \
			/usr/lib/systemd/system/nivaroos-local-storage.service \
			/usr/lib/systemd/system/nivaroos-gpu-sidecar.service \
			/usr/lib/systemd/system/nivaroos-vm-sidecar.service \
			/usr/lib/systemd/system/rclone.service \
			/usr/lib/systemd/system/usb-mount@.service \
			/etc/systemd/system/nivaroos* \
			/etc/udev/rules.d/11-usb-mount.rules \
			/etc/sysctl.d/99-nivaroos.conf \
			/etc/systemd/system/docker.service.d/override.conf
		systemctl daemon-reload
		udevadm control --reload-rules >/dev/null 2>&1 || true
	"
}

remove_binaries() {
	run_step "Removing NivaroOS binaries, CLI tools & symlinks" "
		if [ -f \"$MANIFEST_FILE\" ]; then
			while IFS= read -r f; do
				rm -rf \"\$f\" 2>/dev/null || true
			done < \"$MANIFEST_FILE\"
		fi
		rm -f \
			/usr/bin/nivaroos /usr/bin/nivaroos-gateway /usr/bin/nivaroos-user \
			/usr/bin/nivaroos-app-management /usr/bin/nivaroos-local-storage \
			/usr/bin/nivaroos-message-bus /usr/bin/nivaroos-gpu-sidecar \
			/usr/bin/nivaroos-vm-sidecar /usr/bin/nivaroos-cli /usr/bin/nivaroos-uninstall \
			/usr/local/bin/nivaroos /usr/local/bin/nivaroos-cli /usr/local/bin/nivaroos-uninstall \
			/usr/bin/casaos-cli /usr/bin/casaos /usr/bin/casaos-gateway /usr/bin/casaos-user-service \
			/usr/bin/casaos-app-management /usr/bin/casaos-local-storage /usr/bin/casaos-message-bus 2>/dev/null || true
		rm -rf /var/lib/nivaroos /var/lib/casaos /var/run/nivaroos /etc/nivaroos /usr/share/nivaroos
	"
}

purge_data_if_requested() {
	if [ "$PURGE_DATA" = "yes" ]; then
		run_step "Purging all user data & volumes in /DATA" "rm -rf /DATA"
	else
		run_step "Preserving user data in /DATA" "true"
	fi
}

print_summary() {
	local end_ts
	end_ts=$(date +%s)
	local total_duration=$((end_ts - START_TIME))

	local cols
	cols=$(get_terminal_width)
	local box_width=$((cols - 4))
	if [ "$box_width" -lt 40 ]; then box_width=40; fi

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	local top_title="✔  NivaroOS Successfully Uninstalled! (completed in ${total_duration}s)"
	local top_dashes_len=$((box_width - ${#top_title} - 4))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	printf "\n"
	printf '%b' "${COLOR_GREEN}╭── ${COLOR_BOLD}${COLOR_GREEN}${top_title}${COLOR_RESET}${COLOR_GREEN} ${top_dashes}╮${COLOR_RESET}\n"
	printf '%b' "│                                                                               │\n"
	printf '%b' "│   • All systemd background services have been stopped and disabled.          │\n"
	printf '%b' "│   • System binaries, management tools, and web assets have been removed.      │\n"
	if [ "$PURGE_DATA" = "yes" ]; then
		printf '%b' "│   • ${COLOR_RED}/DATA directory was completely purged.${COLOR_RESET}                                      │\n"
	else
		printf '%b' "│   • ${COLOR_GREEN}/DATA directory was preserved safely.${COLOR_RESET}                                       │\n"
	fi
	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_MUTED}Thank you for using NivaroOS!${COLOR_RESET}                                              │\n"
	printf '%b' "${COLOR_GREEN}╰${bot_dashes}╯${COLOR_RESET}\n\n"
}

main() {
	START_TIME=$(date +%s)
	check_root
	parse_args "$@"
	print_banner
	confirm_uninstall

	info "Beginning NivaroOS teardown..."
	printf "\n"

	stop_services
	remove_unit_files
	remove_binaries
	purge_data_if_requested

	print_summary
}

main "$@"

#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
	printf 'usage: %s <artifact-url> <artifact-digest>\n' "$0" >&2
	exit 1
fi

artifact_url="$1"
artifact_digest="$2"
workdir="$(mktemp -d)"
download_path="$workdir/artifact.bin"
extract_dir="$workdir/extracted"
recovered_dir="$workdir/recovered"

cleanup() {
	rm -rf "$workdir"
}
trap cleanup EXIT

mkdir -p "$extract_dir" "$recovered_dir"

is_zip() {
	local path="$1"
	if [[ "$path" == *.zip ]]; then
		return 0
	fi
	file "$path" | grep -q 'Zip archive data'
}

printf 'Downloading artifact from %s\n' "$artifact_url"
curl -fsSL "$artifact_url" -o "$download_path"

printf 'Expected digest: %s\n' "$artifact_digest"
printf 'Actual sha256: '
sha256sum "$download_path" | cut -d ' ' -f 1

printf '\nDownloaded file type:\n'
file "$download_path"

printf '\nAttempting tar extraction...\n'
if tar -xf "$download_path" -C "$extract_dir" 2>/dev/null; then
	printf 'Tar extraction succeeded\n'
else
	printf 'Tar extraction failed\n'
fi

printf '\nExtracted files:\n'
find "$extract_dir" -maxdepth 4 | sort

module_root=''
if [[ -f "$extract_dir/cue.mod/module.cue" ]]; then
	module_root="$extract_dir"
fi

if [[ -z "$module_root" ]]; then
	if is_zip "$download_path"; then
		unzip -q "$download_path" -d "$recovered_dir/from-download"
		if [[ -f "$recovered_dir/from-download/cue.mod/module.cue" ]]; then
			module_root="$recovered_dir/from-download"
		elif [[ -f "$recovered_dir/from-download/rootfs/cue.mod/module.cue" ]]; then
			module_root="$recovered_dir/from-download/rootfs"
		fi
	fi
fi

if [[ -z "$module_root" ]]; then
	zip_candidates=()
	while IFS= read -r path; do
		if is_zip "$path"; then
			zip_candidates+=("$path")
		fi
	done < <(find "$extract_dir" -type f)

	for candidate in "${zip_candidates[@]:-}"; do
		if [[ -z "$candidate" || ! -f "$candidate" ]]; then
			continue
		fi

		unzip -q "$candidate" -d "$recovered_dir/from-zip"
		if [[ -f "$recovered_dir/from-zip/cue.mod/module.cue" ]]; then
			module_root="$recovered_dir/from-zip"
			break
		fi
		if [[ -f "$recovered_dir/from-zip/rootfs/cue.mod/module.cue" ]]; then
			module_root="$recovered_dir/from-zip/rootfs"
			break
		fi
	done
fi

if [[ -z "$module_root" ]]; then
	printf '\nFailed to recover a valid CUE module tree\n' >&2
	exit 1
fi

printf '\nRecovered module root: %s\n' "$module_root"
printf '\nRecovered tree:\n'
find "$module_root" -maxdepth 4 | sort

if [[ ! -f "$module_root/cue.mod/module.cue" ]]; then
	printf 'Missing cue.mod/module.cue\n' >&2
	exit 1
fi

printf '\nRunning cue export validation...\n'
(
	cd "$module_root"
	cue export ./... || cue export .
)

printf '\nValidation succeeded\n'

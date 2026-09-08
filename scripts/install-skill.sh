#!/bin/sh

set -eu
umask 077

# The preparation route deliberately uses only base-system tools. It never
# resolves a provider directory, downloads a package, or escalates privileges.
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH

stillmac_program=$(basename "$0")
stillmac_stage=
stillmac_manifest_tmp=
stillmac_paths_tmp=
stillmac_claimed_target=0

stillmac_usage() {
    cat >&2 <<'EOF'
usage: install-skill.sh VERSION SOURCE TARGET

Prepare a versioned local StillMac Agent Skill package at an explicit,
absolute TARGET. SOURCE must be the local skill directory containing SKILL.md.
The destination must not already exist.
EOF
}

stillmac_die() {
    printf '%s: %s\n' "$stillmac_program" "$*" >&2
    exit 2
}

stillmac_cleanup() {
    stillmac_status=$?
    trap - EXIT HUP INT TERM
    if [ -n "$stillmac_stage" ] && { [ -e "$stillmac_stage" ] || [ -L "$stillmac_stage" ]; }; then
        rm -R -f "$stillmac_stage" || :
    fi
    if [ -n "$stillmac_manifest_tmp" ] && [ -e "$stillmac_manifest_tmp" ]; then
        rm -f "$stillmac_manifest_tmp" || :
    fi
    if [ -n "$stillmac_paths_tmp" ] && [ -e "$stillmac_paths_tmp" ]; then
        rm -f "$stillmac_paths_tmp" || :
    fi
    if [ "$stillmac_status" -ne 0 ] && [ "$stillmac_claimed_target" -eq 1 ]; then
        printf '%s\n' "$stillmac_program: TARGET was claimed but preparation failed; inspect it before any manual removal" >&2
    fi
    exit "$stillmac_status"
}

trap stillmac_cleanup EXIT HUP INT TERM

if [ "$#" -eq 1 ] && [ "$1" = "--help" ]; then
    stillmac_usage
    exit 0
fi

[ "$#" -eq 3 ] || {
    stillmac_usage
    exit 2
}

stillmac_version=$1
stillmac_source_input=$2
stillmac_target_input=$3

if ! printf '%s\n' "$stillmac_version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
    stillmac_die "VERSION must be canonical vMAJOR.MINOR.PATCH"
fi

case "$stillmac_source_input" in
    /*) ;;
    *) stillmac_die "SOURCE must be an absolute local skill directory" ;;
esac

case "$stillmac_target_input" in
    /*) ;;
    *) stillmac_die "TARGET must be an explicit absolute path" ;;
esac

[ -d "$stillmac_source_input" ] || stillmac_die "SOURCE is not a directory"
[ ! -L "$stillmac_source_input" ] || stillmac_die "SOURCE must not be a symlink"

stillmac_source_abs=$(cd -P "$stillmac_source_input" && pwd -P) || stillmac_die "SOURCE cannot be resolved"
stillmac_target_name=$(basename "$stillmac_target_input")
stillmac_target_parent_input=$(dirname "$stillmac_target_input")

case "$stillmac_target_name" in
    ""|.|..) stillmac_die "TARGET must name a skill directory" ;;
esac

[ -d "$stillmac_target_parent_input" ] || stillmac_die "TARGET parent must already exist"
[ ! -L "$stillmac_target_parent_input" ] || stillmac_die "TARGET parent must not be a symlink"
stillmac_target_parent_abs=$(cd -P "$stillmac_target_parent_input" && pwd -P) || stillmac_die "TARGET parent cannot be resolved"
stillmac_target_abs=$stillmac_target_parent_abs/$stillmac_target_name

case "$stillmac_source_abs" in
    *[[:cntrl:]]*) stillmac_die "SOURCE path must not contain control characters" ;;
esac
case "$stillmac_target_abs" in
    *[[:cntrl:]]*) stillmac_die "TARGET path must not contain control characters" ;;
esac

stillmac_uid=$(id -u)
stillmac_check_directory() {
    stillmac_check_path=$1
    stillmac_check_label=$2
    [ -d "$stillmac_check_path" ] || stillmac_die "$stillmac_check_label is not a directory"
    [ ! -L "$stillmac_check_path" ] || stillmac_die "$stillmac_check_label must not be a symlink"
    [ "$(stat -f '%u' "$stillmac_check_path" 2>/dev/null || stat -c '%u' "$stillmac_check_path")" = "$stillmac_uid" ] || stillmac_die "$stillmac_check_label is not owned by the invoking user"
    [ -z "$(find "$stillmac_check_path" -prune -perm -022 -print -quit)" ] || stillmac_die "$stillmac_check_label is group or world writable"
}

stillmac_check_control_names() {
    stillmac_name_root=$1
    stillmac_name_label=$2
    [ -z "$(find "$stillmac_name_root" -name '*[[:cntrl:]]*' -print -quit)" ] || stillmac_die "$stillmac_name_label contains a control character in a file name"
}

stillmac_check_directory "$stillmac_source_abs" "SOURCE"
stillmac_check_directory "$stillmac_target_parent_abs" "TARGET parent"

if [ -e "$stillmac_target_abs" ] || [ -L "$stillmac_target_abs" ]; then
    stillmac_die "TARGET already exists and will not be overwritten"
fi

case "$stillmac_target_abs" in
    "$stillmac_source_abs"|"$stillmac_source_abs"/*)
        stillmac_die "TARGET must not be inside SOURCE"
        ;;
esac

stillmac_skill_file=$stillmac_source_abs/SKILL.md
[ -f "$stillmac_skill_file" ] || stillmac_die "SOURCE must contain a regular SKILL.md"
[ ! -L "$stillmac_skill_file" ] || stillmac_die "SKILL.md must not be a symlink"
[ ! -e "$stillmac_source_abs/.stillmac-install.json" ] || stillmac_die "SOURCE contains a reserved install receipt"
[ ! -L "$stillmac_source_abs/.stillmac-install.json" ] || stillmac_die "SOURCE contains a reserved install receipt"

stillmac_unsafe_source=$(find "$stillmac_source_abs" \( -type l -o -type b -o -type c -o -type p -o -type s \) -print -quit)
[ -z "$stillmac_unsafe_source" ] || stillmac_die "SOURCE contains a symlink or special file"
stillmac_unsafe_source=$(find "$stillmac_source_abs" -perm -022 -print -quit)
[ -z "$stillmac_unsafe_source" ] || stillmac_die "SOURCE contains a group or world writable entry"
stillmac_check_control_names "$stillmac_source_abs" "SOURCE"

if awk 'index($0, "\r") { found=1; exit } END { exit(found ? 0 : 1) }' "$stillmac_skill_file"; then
    stillmac_die "SKILL.md must use LF line endings"
fi

[ "$(sed -n '1p' "$stillmac_skill_file")" = "---" ] || stillmac_die "SKILL.md must start with YAML frontmatter"
stillmac_front_end=$(awk 'NR > 1 && $0 == "---" { print NR; exit }' "$stillmac_skill_file")
[ -n "$stillmac_front_end" ] || stillmac_die "SKILL.md YAML frontmatter is not closed"

stillmac_skill_name=$(awk -v end="$stillmac_front_end" '
    NR > 1 && NR < end && $0 ~ /^name:[[:space:]]*/ {
        value=$0
        sub(/^name:[[:space:]]*/, "", value)
        count++
        if (count == 1) first=value
    }
    END {
        if (count != 1 || first == "") exit 1
        print first
    }
' "$stillmac_skill_file") || stillmac_die "SKILL.md must contain one non-empty name field"

printf '%s\n' "$stillmac_skill_name" | grep -Eq '^[a-z0-9]+(-[a-z0-9]+)*$' || stillmac_die "SKILL.md name is not a lowercase hyphenated name"
[ "$(printf '%s\n' "$stillmac_skill_name" | awk '{ print length($0) }')" -le 64 ] || stillmac_die "SKILL.md name is longer than 64 characters"
[ "$(basename "$stillmac_source_abs")" = "$stillmac_skill_name" ] || stillmac_die "SOURCE directory name must match SKILL.md name"
[ "$stillmac_target_name" = "$stillmac_skill_name" ] || stillmac_die "TARGET directory name must match SKILL.md name"

stillmac_skill_description=$(awk -v end="$stillmac_front_end" '
    NR > 1 && NR < end && $0 ~ /^description:[[:space:]]*/ {
        value=$0
        sub(/^description:[[:space:]]*/, "", value)
        count++
        if (count == 1) first=value
    }
    END {
        if (count != 1 || first == "") exit 1
        print first
    }
' "$stillmac_skill_file") || stillmac_die "SKILL.md must contain one non-empty description field"
[ -n "$stillmac_skill_description" ]

stillmac_release_line="  cli-release: \"$stillmac_version\""
stillmac_release_count=$(awk -v end="$stillmac_front_end" -v wanted="$stillmac_release_line" 'NR > 1 && NR < end && $0 == wanted { count++ } END { print count + 0 }' "$stillmac_skill_file")
[ "$stillmac_release_count" -eq 1 ] || stillmac_die "SKILL.md cli-release metadata does not match VERSION"

stillmac_hash_file() {
    if [ "$(uname -s)" = "Darwin" ]; then
        shasum -a 256 "$1" | awk '{ print $1 }'
    else
        sha256sum "$1" | awk '{ print $1 }'
    fi
}

stillmac_tree_digest() {
    stillmac_tree_root=$1
    stillmac_manifest_tmp=$(mktemp "$stillmac_target_parent_abs/.stillmac-manifest.XXXXXX") || stillmac_die "cannot create a private digest manifest"
    stillmac_paths_tmp=$(mktemp "$stillmac_target_parent_abs/.stillmac-paths.XXXXXX") || stillmac_die "cannot create a private path list"
    chmod 600 "$stillmac_manifest_tmp" "$stillmac_paths_tmp"
    find "$stillmac_tree_root" \( -type d -o -type f \) -print | LC_ALL=C sort > "$stillmac_paths_tmp"

    while IFS= read -r stillmac_entry_path; do
        case "$stillmac_entry_path" in
            "$stillmac_tree_root") stillmac_entry_rel=. ;;
            "$stillmac_tree_root"/*)
                stillmac_entry_rel=$(printf '%s\n' "$stillmac_entry_path" | awk -v prefix="$stillmac_tree_root/" 'index($0, prefix) == 1 { print substr($0, length(prefix) + 1) }')
                ;;
            *) stillmac_die "digest encountered an unexpected path" ;;
        esac
        case "$stillmac_entry_rel" in
            .stillmac-install.json) continue ;;
        esac
        if [ -d "$stillmac_entry_path" ]; then
            printf 'd\t%s\n' "$stillmac_entry_rel" >> "$stillmac_manifest_tmp"
        else
            stillmac_entry_executable=0
            [ -x "$stillmac_entry_path" ] && stillmac_entry_executable=1
            stillmac_entry_hash=$(stillmac_hash_file "$stillmac_entry_path")
            printf 'f\t%s\t%s\t%s\n' "$stillmac_entry_executable" "$stillmac_entry_rel" "$stillmac_entry_hash" >> "$stillmac_manifest_tmp"
        fi
    done < "$stillmac_paths_tmp"

    stillmac_tree_hash=$(stillmac_hash_file "$stillmac_manifest_tmp")
    rm -f "$stillmac_manifest_tmp" "$stillmac_paths_tmp"
    stillmac_manifest_tmp=
    stillmac_paths_tmp=
    printf '%s\n' "$stillmac_tree_hash"
}

stillmac_stage=$(mktemp -d "$stillmac_target_parent_abs/.stillmac-skill.XXXXXX") || stillmac_die "cannot create a private staging directory"
chmod 700 "$stillmac_stage"
cp -R "$stillmac_source_abs"/. "$stillmac_stage" || stillmac_die "cannot stage SOURCE"
chmod -R u+rwX,go-rwx "$stillmac_stage"

stillmac_unsafe_stage=$(find "$stillmac_stage" \( -type l -o -type b -o -type c -o -type p -o -type s \) -print -quit)
[ -z "$stillmac_unsafe_stage" ] || stillmac_die "staging produced a symlink or special file"
stillmac_check_control_names "$stillmac_stage" "staging"

stillmac_payload_hash=$(stillmac_tree_digest "$stillmac_stage")
stillmac_receipt=$stillmac_stage/.stillmac-install.json
printf '%s\n' "{\"schema_version\":\"stillmac.skill-install.v1\",\"skill_name\":\"$stillmac_skill_name\",\"release_version\":\"$stillmac_version\",\"payload_sha256\":\"$stillmac_payload_hash\"}" > "$stillmac_receipt"
chmod 600 "$stillmac_receipt"
chmod 700 "$stillmac_stage"

# mkdir is the exact-target ownership claim. Unlike a move into a directory,
# it cannot nest the private stage under a destination that appeared late.
if ! mkdir "$stillmac_target_abs" 2>/dev/null; then
    stillmac_die "TARGET appeared during preparation and was not modified"
fi
stillmac_claimed_target=1
chmod 700 "$stillmac_target_abs"
cp -R "$stillmac_stage"/. "$stillmac_target_abs"/ || stillmac_die "TARGET was claimed but SOURCE could not be copied"
chmod -R u+rwX,go-rwx "$stillmac_target_abs"

[ -d "$stillmac_target_abs" ] || stillmac_die "prepared TARGET is not a directory"
[ "$(stat -f '%u' "$stillmac_target_abs" 2>/dev/null || stat -c '%u' "$stillmac_target_abs")" = "$stillmac_uid" ] || stillmac_die "prepared TARGET is not owned by the invoking user"
[ -z "$(find "$stillmac_target_abs" -perm -022 -print -quit)" ] || stillmac_die "prepared TARGET is group or world writable"
[ "$(stillmac_tree_digest "$stillmac_target_abs")" = "$stillmac_payload_hash" ] || stillmac_die "prepared TARGET failed deterministic verification"

rm -R -f "$stillmac_stage"
stillmac_stage=

printf 'Prepared StillMac Agent Skill %s at %s.\n' "$stillmac_version" "$stillmac_target_abs"
printf '%s\n' 'No provider directory, Codex configuration, network resource, or privilege escalation was used.'
printf '%s\n' 'The destination was absent and was not overwritten.'

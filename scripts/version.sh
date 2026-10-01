#!/bin/bash
#
# Version management.

set -euo pipefail

# Strict semver, nothing else: release.yml fires on `v[0-9]+.[0-9]+.[0-9]+`
# alone, so a prerelease or build-metadata tag would be created and pushed and
# publish nothing -- a silent no-op release.
VERSION_REGEX="^v?[0-9]+\.[0-9]+\.[0-9]+$"

# Latest released version, without the leading `v`. Empty if there is none.
#
# `|| true`: grep exits 1 on no tags, which pipefail and set -e would turn into a silent exit.
get_latest_version() {
    git tag -l "v*" | { grep -E "$VERSION_REGEX" || true; } | sed 's/^v//' | sort -V | tail -1
}

bump_version() {
    local version=$1
    local type=$2

    echo "$version" | awk -F. -v type="$type" '{
        if (type == "major") {
            print $1+1".0.0"
        } else if (type == "minor") {
            print $1"."$2+1".0"
        } else if (type == "patch") {
            print $1"."$2"."$3+1
        } else {
            print "invalid"
        }
    }'
}

# determine_version [VERSION] [TYPE]
#
# With VERSION, validates and echoes it. Without, bumps the latest tag by TYPE.
# The first release of a repository with no tags is 1.0.0.
determine_version() {
    local version=${1:-}
    local type=${2:-}

    if [ -z "$version" ]; then
        local current
        current=$(get_latest_version)
        if [ -z "$current" ]; then
            echo "1.0.0"
            return
        fi
        if [ -z "$type" ]; then
            echo "Error: TYPE must be specified (major, minor, or patch) when VERSION is not provided" >&2
            exit 1
        fi
        local bumped
        bumped=$(bump_version "$current" "$type")
        if [ "$bumped" = "invalid" ]; then
            echo "Error: Invalid version bump type '$type'. Must be major, minor, or patch" >&2
            exit 1
        fi
        echo "$bumped"
    else
        version=$(echo "$version" | sed 's/^v//')
        if ! echo "$version" | grep -Eq "$VERSION_REGEX"; then
            echo "Error: Invalid version format '$version'. Must match semver format (e.g., 1.0.0)" >&2
            exit 1
        fi
        echo "$version"
    fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    determine_version "$@"
fi

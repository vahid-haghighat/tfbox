#!/bin/bash

# Fetch the latest tag from the current repo
latest_tag=$(git describe --tags $(git rev-list --tags --max-count=1))

# Update the version file with the latest tag
printf "%s" "$latest_tag" > version/version
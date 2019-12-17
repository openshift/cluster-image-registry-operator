#!/bin/sh
set -eu

if [ -n "${JUNIT_REPORT-}" ]; then
    if [ -z "${ARTIFACT_DIR-}" ]; then
        printf >&2 "JUNIT_REPORT=1 requires ARTIFACT_DIR to be set\n"
        exit 1
    fi

    mkdir -p "$ARTIFACT_DIR"
    GO_TEST_JSON="$ARTIFACT_DIR/go_test.json"
    REPORT_FILE="$ARTIFACT_DIR/junit_go_test.xml"

    _V="-v"
    for i do [ "$i" != "-v" ] || _V=""; done
    RETCODE=0
    go test $_V -json "$@" >"$GO_TEST_JSON" || RETCODE=$?

    ! grep "^[^{]" "$GO_TEST_JSON" || exit $RETCODE
    go run "$(dirname "$0")/junitxml/junitxml.go" <"$GO_TEST_JSON" >"$REPORT_FILE"
    gzip "$GO_TEST_JSON"

    exit $RETCODE
else
    go test "$@"
fi

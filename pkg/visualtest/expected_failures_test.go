package visualtest

// expectedFailuresPath is the manifest of WPT reftests that louis14 is expected
// to FAIL because Blink — louis14's canonical reference engine (CLAUDE.md
// "Blink is canonical") — also fails them while Gecko passes. Such tests encode
// behavior our reference engine does not implement, so matching Blink means
// failing the WPT reference. The reftest runner reports these as XFAIL instead
// of FAIL, and ALERTS (fails the suite) if one starts passing. See the file
// header in expected_failures.txt for the full policy and wpt.fyi provenance.
//
// Path is relative to the package directory (go test's working directory).
const expectedFailuresPath = "expected_failures.txt"

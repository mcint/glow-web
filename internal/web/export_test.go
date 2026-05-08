package web

// FormatListenerLinesForTest exposes the unexported formatter so tests in
// web_test (external test package) can drive it with synthetic IP lists.
var FormatListenerLinesForTest = formatListenerLines

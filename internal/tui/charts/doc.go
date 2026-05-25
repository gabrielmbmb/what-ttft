// Package charts contains string chart adapters for the live terminal dashboard.
//
// Renderers in this package are pure functions: they do not read terminal
// state, mutate benchmark records, perform provider requests, or write files.
// The main chart adapters isolate ntcharts-backed line and bar primitives behind
// what-ttft-specific options, while compact fallback renderers remain fully
// deterministic for small terminal boxes and focused unit tests.
//
// All charts label units explicitly and avoid token-level terminology unless
// true token timestamps exist in a future milestone.
package charts

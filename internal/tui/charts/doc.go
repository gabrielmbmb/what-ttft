// Package charts contains deterministic string renderers for the live terminal dashboard.
//
// Renderers in this package are pure functions: they do not read terminal
// state, mutate benchmark records, perform provider requests, or write files.
// They label units explicitly and avoid token-level terminology unless true
// token timestamps exist in a future milestone.
package charts

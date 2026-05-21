// Package tui renders live benchmark events in a Bubble Tea terminal dashboard.
//
// The package consumes whatttft.RunEvent values and presents them to users. It
// must not perform provider requests, write report files, or calculate
// benchmark latency metrics itself; canonical timing and result data remain in
// the runner records and report files.
package tui

// Package timeline holds the timeline job. It walks the first-parent history
// of each target's indexed branch, diffs every changed manifest into
// dependency events and folds the events into version intervals.
package timeline

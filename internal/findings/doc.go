// Package findings holds findings.Service: acknowledge, snooze and dismiss a
// finding. The state rules live on model.Finding; this package loads the
// finding, asks the model, and stores the new state.
package findings

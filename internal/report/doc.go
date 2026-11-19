// Package report is the digest job: it ranks what changed since the last
// digest, builds one model.Digest and sends it through every enabled
// Notifier, recording each send in the digests table.
package report

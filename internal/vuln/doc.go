// Package vuln is the vulns job: it queries the advisory database for every
// pinned package version, matches advisories against dependency intervals with
// an exposure window and a confidence label, enriches advisories with KEV and
// EPSS, and raises advisory findings. Adapters live in vuln/osv, vuln/kev and
// vuln/epss.
package vuln

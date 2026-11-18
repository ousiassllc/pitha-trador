// Package guards holds the tests that pin the dataset export's reads (every statement uses an index)
// and its isolation from trading code (issue #1094). It lives apart from datasetexport so that package
// stays within the repository's directory size limit; it has no production code.
package guards

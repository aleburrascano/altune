// Package handler is the HTTP surface of the admin console, mounting the
// operator-only routes over every other package in this module. It holds
// AdminHandler and its one-file-per-resource routes, the two-principal gate
// that admits them (OperatorOnly, OperatorOrReadOnly), the codedError
// vocabulary every admin failure answers with, the shared SSE plumbing the
// live tails stream over, and the admission gate bounding the inspector
// replay routes.
package handler

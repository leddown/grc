// Package policystudio is the Policy Studio: collaborative WYSIWYG authoring
// for Studio-format policy documents (see POLICY_STUDIO.md).
//
// The server holds the only authoritative copy of a draft. Each Studio
// document is a Yjs document in an in-process ygo room; the editor in the
// browser (web/policy-studio, embedded here as a hashed bundle) edits it, and
// this package projects it -- validated against the schema in schema.go --
// into the policy_sections rows that lint, knowledge, exports and approval
// read. Nothing a client sends is trusted as content: what reaches the rows is
// what the server read from its own document and validated.
package policystudio

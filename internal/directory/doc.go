// Package directory authenticates a person against an external directory and
// reports who they are — nothing more.
//
// It answers one question: "does this identifier and password belong to
// someone in your directory, and what is their stable identity?" It does not
// decide what anyone may do. That stays where ADR 0014 and ADR 0017 put it:
// project membership, organization membership and installation_admins
// (docs/adr/0019). A directory that could grant capabilities would mean an
// administrator of the directory is an administrator of this installation,
// which is a different product.
//
// Nor does it touch the database. Provisioning a local user for a directory
// identity is internal/identity's job; this package has no pool, no
// transaction and no knowledge of users. The split matters because it is what
// lets the LDAP path and the OIDC path that follows it share everything after
// the identity is established.
package directory

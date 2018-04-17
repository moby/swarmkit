package errors

// The errors package is a package that holds errors common across the various
// network allocator components. The purpose of using its own package like this
// is to ensure that common errors can be propagated through the network
// allocator and up to any caller without modification at any level, and
// without creating unnecessary or circular dependencies.

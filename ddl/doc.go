// Package ddl projects an aggregate's description into the tables that hold it.
//
// An aggregate is not one table. An object with an identity is an entity and
// gets a table of its own; an object without one is a value belonging to
// whatever holds it, and lives in that thing's row. So a root with a list of
// entities beneath it projects to several tables and the foreign keys between
// them, and the derivation says which.
//
// Two dialects, and they are not one dialect with different keywords. Postgres
// has no unsigned integers, MySQL's boolean is a tinyint, identity is a serial
// against an auto-increment, and only Postgres has transactional DDL -- which
// changes how a migration is applied and not only how a column is spelled. So a
// Dialect resolves the types and renders the statements, and where a dialect
// cannot express what the description says, this **refuses** rather than
// approximating: silently turning a caller's unsigned 64-bit key into a decimal
// changes the type every other language generates from the same description,
// which is the mistake protobuf's field numbers exist to prevent.
//
// What is here is CREATE. Migrations are the larger half and the plan's section
// 7 records the approach: declared steps between pinned versions rather than a
// diff against whatever is there.
package ddl

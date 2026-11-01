// Package ports is what the gateway's core needs of the world, as
// interfaces and the plain types they carry (§6.5.3, constitution rule 8:
// dependencies point inward). An adapter implements a port for one real
// system; a fake implements it in memory, at the same seam; every
// implementation runs the port's suite in porttest. Nothing here holds
// logic, so a company plugs in Snowflake, Entra or its own audit store by
// writing one package against one interface.
package ports

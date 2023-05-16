package mux

/*
mux implements support for multiplexing many public endpoints to the same backend;
more specifically this implementation allows to expose many host:port listeners
that will be processed by a single handler.

The implementation is inspired from https://fideloper.com/golang-proxy-multiple-listeners
(kudos to the author!)
*/

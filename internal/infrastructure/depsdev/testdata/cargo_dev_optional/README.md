This simplified crate is a hand-written fixture, not a real Cargo resolution.
For example, the dev-only serde `derive` feature and `serde_derive` are absent from the lockfile and graph.
The tests serve the two JSON files as deps.dev responses and define expected counts against that synthetic graph.
uzomuzo itself never reads Cargo.toml or Cargo.lock.

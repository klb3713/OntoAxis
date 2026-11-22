// Package runtime implements the connectors Kafka Connect/CDC runtime: deterministic
// runtime connector naming, Debezium source and Iceberg sink config generation,
// secret resolution, a Kafka Connect REST client, and the submit/status/stop
// orchestration used by the sync dispatcher.
//
// Credentials are injected from a SecretResolver only while building submit
// requests. They must never be persisted, logged, or embedded in errors, metrics,
// traces, or HTTP responses.
package runtime

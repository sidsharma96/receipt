# Self-managed, single-node PostgreSQL; not Fly Managed Postgres.
# Resolve and record a current patch-image digest before first deployment.
FROM postgres:17-bookworm
CMD ["postgres", "-c", "listen_addresses=*", "-c", "max_connections=40", "-c", "shared_buffers=128MB"]

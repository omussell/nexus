#!/bin/sh
set -e

echo "nauvis: running batch ingest..."
/nauvis -db /data/nauvis.sqlite3 -in /data/in -out /data/out -croid http://croid:8080 -jobs 8

echo "nauvis: starting HTTP server..."
exec /nauvis -serve -host :8080 -db /data/nauvis.sqlite3 -in /data/in -out /data/out -croid http://croid:8080

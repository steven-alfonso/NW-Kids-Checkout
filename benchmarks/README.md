# Checkouts benchmark harness

Local comparison harness for the framework spikes. It uses the same fake API
and static server as the conformance suite.

Run one implementation at a time:

```sh
npm run bench          # unthrottled
npm run bench:cpu4     # 4x CPU throttle
```

Results are written to `benchmarks/results-<name>.json`.

Metrics:

- first paint, FCP, DCL, load, and board-ready time for 7 and 100 rows
- poll update latency at 7 and 100 rows
- 1s tick cost at 100 rows
- overdue sheet open/close, search, and confirmation latency
- bundle bytes and gzip bytes

The harness is not a CI gate. Absolute numbers depend on the machine; compare
runs from the same session and machine.

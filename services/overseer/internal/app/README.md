`app` wires the overseer server from its modules and runs it.

- `New` takes the settings, the buckets and the environment reader, and `Run` starts the collect loop, serves the dashboard and joins the started buckets before it closes history.
- `Buckets` builds the nine buckets from the settings.
- This is the only module besides `cmd/overseer` that may import a bucket or the shell, so it is where features meet.

# Express conversion pilot

The first source-to-Frame converter is intentionally small. It proves equivalence for one
reviewable Express subset instead of replacing unknown code with successful placeholders.

## Supported matrix

| Item | Qualified pilot |
|---|---|
| Runtime | Node.js 20 declared as `"engines": {"node": "20"}` |
| Framework | Exact production dependency `express: 4.21.2` |
| Module form | One CommonJS `.js` or `.cjs` entrypoint |
| Start script | `node <relative-entry>` invoked by `npm start` |
| Routes | Fixed GET, POST, PUT, PATCH and DELETE paths |
| Responses | Constant JSON literals or constant double-quoted strings; optional fixed status |
| String types | Express default HTML, `text/plain`, or `text/html` |
| Assets | Fixed `express.static` mounts, 5,000 files/32 MiB total/10 MiB per file |
| Environment | Names from `.env.example`; values are never copied |

All application statements must fit this subset. The converter rejects request-dependent
handlers, route parameters, imported business modules, additional server source files, extra
production dependencies, middleware, database access, templates, WebSockets, custom headers,
dynamic response expressions, unsupported asset files and overlapping routes/mounts. Flask and
every other framework remain as-is only.

## Review and apply

Planning is the default and does not write files:

```sh
quikdb-frame convert ./my-app --from express --json > conversion-review.json
```

After reviewing the routes, response bytes, assets, environment names, source hashes and stated
limits, explicitly apply the conversion:

```sh
quikdb-frame convert ./my-app --from express --output ./my-app-frame --apply
```

The output is a single native Go HTTP service built into a scratch image. It includes:

- `conversion/conversion-plan.json`, the deterministic reviewed contract;
- `conversion/as-is-quikdb.json`, the original Node runtime/startup configuration;
- `quikdb.yaml`, the native Frame project/build contract;
- generated Go routes with literal response bytes and embedded copies of declared assets; and
- `.env.example` containing names only.

The original source directory is never modified. If conversion review or parity testing fails,
continue deploying that original source with `--mode as-is` and its generated as-is manifest.

## What parity means

Hosted qualification starts the original Express fixture and the converted binary independently.
For every declared oracle request it compares HTTP status, normalized content type and exact body
bytes; it also compares declared static asset bytes. It builds both container images and requires
the scratch conversion to be smaller than the original fixture image.

The pilot does not claim parity for transport-generated headers, undeclared paths, dynamic input,
state, persistence or any Express feature outside the table. A successful plan is not permission
to deploy; users must review, apply and test the generated artifact first.

# Contribute to Receipt

Clone the repository, build the executable and run the tests:

```sh
git clone https://github.com/sidsharma96/receipt.git
cd receipt
make build
make check
make race
```

Go builds the application and the repository's maintenance tools. Python is not
needed for these workflows; Python workloads still need their own interpreter
and packages. Read the [coding practices](CODING-PRACTICES.md) before changing Go.

Before editing prose, read the [writing guide](PROSE-STYLE-GUIDE.md). Review the
explanation without source code first, then verify its claims against the code.
Check that chapter exercises use concepts already taught. Repair editorial gaps
before continuing; the checks below cannot establish that an explanation teaches.

## Check a change

`make check` runs the pinned linter and unit tests, including maintenance-tool
tests. `make race` checks exercised concurrent code for data races. Database tests
require a disposable PostgreSQL database through `RECEIPT_TEST_DATABASE_URL`;
`make integration` refuses to run without that setting. Never point tests at the
deployed database.

For documentation, install mdBook 0.5.4 alongside Go:

```sh
go run ./tools/docs sync
make docs
```

Edit questions in `docs/questions/`, not their generated chapter copies. New
book pages belong in `docs/SUMMARY.md`. Edit illustrations directly in
`docs/assets/`; their SVG files are the maintained source. Preserve the evidence
links and sample limits when changing a measured chart. The checks validate SVG
syntax, links, code excerpts and questions; they do not recalculate chart values.
Inspect the rendered page at desktop and phone widths after changing a figure.

## Keep source and generated output separate

Shared Go maintenance commands live in `tools/`. Small shell scripts and
YAML/TOML configuration remain versioned where their workflows need them.
Disposable personal helpers, including Python, belong in ignored `scripts/utils/`;
other contributors will not have those files. Builds, docs and CI must not depend
on that directory. Do not hide shared code in an ignored file or embed Python in
shell scripts to avoid the source policy.

Store raw measurements in ignored `.tools/` or CI artifacts. Do not commit JSON,
JSONL, NDJSON, Python source/bytecode or raw profiler captures. Individual source
files must stay within 1 MiB. The application's JSON formats and support for Python
workloads are unaffected.

Enable the local commit hook once per clone:

```sh
make hooks-install
```

Do not replace a custom hook configuration silently. Before committing, inspect
`git diff --cached` and run the relevant checks. The hook runs
`go run ./tools/artifacts --staged` against the actual staged files, then checks
whitespace; it does not replace tests. CI repeats the artifact check.

## Measurements and operations

Use the [local measurement walkthrough](MEASUREMENTS.md#compare-reuse-with-running-the-tests)
to compare direct execution with Receipt. Completed profiling experiments and
recovery-drill scripts have been retired from the current checkout; their source
remains in [Git history](https://github.com/sidsharma96/receipt/tree/88b6edf3c78c1f681735cb893c355b2ab45ae562/scripts/profile).
Older raw records remain linked in the [measurement sources](PERFORMANCE.md#measurement-sources).
Normal builds and docs checks need neither the historical tools nor those records.

For a release, follow the
[release procedure](https://github.com/sidsharma96/receipt/blob/main/scripts/release/README.md).
For server work, use the [operator guide](DIGITALOCEAN-SETUP.md).

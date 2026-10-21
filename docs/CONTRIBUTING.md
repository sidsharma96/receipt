# Contribute to Receipt

Clone the repository, build the executable and run the tests:

```sh
git clone https://github.com/sidsharma96/receipt.git
cd receipt
make build
make check
make race
```

Go builds the application. Python supports documentation checks and CI helpers;
it is not an application runtime dependency. Read the
[coding practices](CODING-PRACTICES.md) before changing Go code.

Before editing prose, read the [writing guide](PROSE-STYLE-GUIDE.md). Review the
explanation without source code first, then verify its claims against the code.
Check that chapter exercises use concepts already taught. Repair editorial gaps
before continuing; the checks below cannot establish that an explanation teaches.

## Check a change

`make check` runs the pinned linter and unit tests. `make race` checks exercised
concurrent code for data races. Database tests require a disposable PostgreSQL
database through `RECEIPT_TEST_DATABASE_URL`; `make integration` refuses to run
without that setting. Never point tests at the deployed database.

For documentation, install mdBook 0.5.4 and use Python 3.11 or later:

```sh
python3 scripts/docs/sync_questions.py
make docs
```

Edit questions in `docs/questions/`, not their generated chapter copies. New
book pages belong in `docs/SUMMARY.md`. Read the [prose guide](PROSE-STYLE-GUIDE.md),
then inspect the rendered page: valid links do not establish a useful explanation.

## Keep source and generated output separate

Keep shared scripts while builds, CI or documented procedures need them. Put
disposable personal helpers in ignored `scripts/utils/`; other contributors will
not have those files. Store raw measurements in ignored `.tools/` or CI artifacts.
Do not commit JSON, JSONL, NDJSON or raw profiler captures. Individual source
files must stay within 1 MiB. The application's JSON formats are unaffected.

Enable the local commit hook once per clone:

```sh
make hooks-install
```

Do not replace a custom hook configuration silently. Before committing, inspect
`git diff --cached` and run the relevant checks. The hook checks the actual staged
files and whitespace; it does not replace tests. CI repeats the artifact check.

## Optional investigation tools

`scripts/profile/` contains shared measurement tools. Older raw records are
available through the [measurement sources](PERFORMANCE.md#measurement-sources).
Normal builds and docs checks do not need them. Restore optional evidence with:

```sh
python3 scripts/profile/restore_measurements.py
```

The helper reads local Git objects, makes no network request, and refuses to
overwrite different measurement contents. A shallow clone may lack the needed
history; consult the helper's error before fetching more objects.

For a release, follow the
[release procedure](https://github.com/sidsharma96/receipt/blob/main/scripts/release/README.md).
For server work, use the [operator guide](DIGITALOCEAN-SETUP.md).

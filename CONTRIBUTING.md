# Contributing

Thank you for helping. The rules below keep the history readable and the gate meaningful.

## Before you start

- Read `AGENTS.md` (the map and the hard rules of this repository) and the README.
- Open or find an issue for anything larger than a small fix, so the approach is agreed first.

## Making a change

1. Branch from the default branch: `<type>/<kebab-english-description>`, for example `fix/webhook-retry`.
2. Keep the change focused. Add or update tests for new behaviour, including the failure cases.
3. Run the repository's gate and keep it green: `bash cong/kiem-kho.sh` and `bash cong/kiem-conformance.sh` (see the README for flags). Do not skip failing tests to pass it.
4. Commit and title the pull request as `type(scope): subject`. Types: `feat`, `fix`, `perf`, `refactor`, `test`, `docs`, `build`, `ci`, `chore`, `revert`. The subject is imperative, lower case, has no trailing full stop and is at most 72 characters; write it in the language the repository's history already uses (English by default for new repositories).
5. Open a pull request using the template. It is squash-merged, so the pull request title becomes the commit title.

## Rules that are not negotiable

- No secrets, tokens, keys or `.env` files in commits.
- No rewriting of published history.
- No personal data in code, tests, examples or logs.
- Changes that touch money, access control or stored personal data need an independent review.

## Licensing

By contributing you agree that your contribution is licensed under the license of this repository (see `LICENSE`).

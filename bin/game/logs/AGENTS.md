# Bug report diagnostics

- Put every extracted bug report under `bin/game/logs/bugs`. Give each report
  or related batch its own clearly named subdirectory; do not extract bug
  reports directly into `bin/game/logs`.
- Keep unrelated reverse-engineering output and generated content diagnostics
  outside `bin/game/logs/bugs` so the bug-report cleanup boundary stays clear.
- After completing a bug fix, delete the corresponding extracted report,
  source archive, and other report-specific artifacts from
  `bin/game/logs/bugs`. Do not retain resolved reports as an archive; Git and
  the durable implementation notes are the history.
- Do not delete reports for unresolved bugs or reports that are still needed
  to verify another active issue in the same batch.

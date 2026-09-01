. as $root |
[
  "Documentation, vet, and secrets",
  "Tests (ubuntu-latest)",
  "Tests (windows-latest)",
  "Tests (macos-latest)",
  "Race detector",
  "Cross-build six supported targets",
  "Package one native-install asset batch",
  "Native install (linux-amd64, full)",
  "Native install (linux-arm64, smoke)",
  "Native install (windows-amd64, full)",
  "Native install (windows-arm64, smoke)",
  "Native install (darwin-amd64, smoke)",
  "Native install (darwin-arm64, full)"
] as $expected |
($root.total_count <= 100) and
($expected | all(.[];
  . as $name |
  ($root.check_runs |
    map(select(
      .name == $name and
      .head_sha == $sha and
      .app.id == 15368
    )) |
    sort_by(.started_at, .id) |
    last
  ) as $run |
  $run != null and
  $run.status == "completed" and
  $run.conclusion == "success"
))

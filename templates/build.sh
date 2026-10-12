#!/usr/bin/env bash
# build.sh — render the document templates to PDF.
#
#   ./build.sh                          both Typst templates, sample data
#   ./build.sh policy                   just the policy document
#   ./build.sh report                   just the business report
#   ./build.sh latex                    the LaTeX policy document
#   ./build.sh policy path/to/doc.json  a real document
#
# Environment:
#   TYPST      path to the typst binary        (default: typst on PATH)
#   TECTONIC   path to tectonic, or set LATEX  (default: tectonic on PATH)
#   LATEX      latex driver for the .tex path  (default: tectonic -X compile)
#   PDF_STD    comma-separated PDF standards   (default: a-2b for archival)
#
# PDF_STD is worth understanding rather than ignoring. Policy documents get
# filed and re-read years later, so a-2b (PDF/A-2b) is the default: it embeds
# every font and forbids the features that make a PDF render differently on a
# machine that no longer has the original fonts. Add ua-1 for tagged,
# screen-reader-accessible output — Typst checks heading order and alt text and
# will fail the build if the document is not actually accessible, which is the
# point. Note that ua-1 and a-4 are mutually exclusive: PDF/UA-1 requires PDF
# 1.7 or earlier, PDF/A-4 requires 2.0.

set -euo pipefail

cd "$(dirname "$0")"

TYPST="${TYPST:-typst}"
TECTONIC="${TECTONIC:-tectonic}"
PDF_STD="${PDF_STD:-a-2b}"
OUT="out"

mkdir -p "$OUT"

have() { command -v "$1" >/dev/null 2>&1 || [ -x "$1" ]; }

build_typst() {
  local name="$1" data="$2"
  if ! have "$TYPST"; then
    echo "typst not found. Install it (https://github.com/typst/typst/releases)" >&2
    echo "or set TYPST=/path/to/typst. It is a single static binary." >&2
    return 1
  fi
  echo "==> $name  (data: $data)"
  # --root .. so the template may reference ../samples/*.json; Typst refuses to
  # read outside the project root by design.
  #
  # The grep drops only "unknown font family" warnings. Those are expected and
  # not actionable: brand.typ lists fallback chains like
  # ("Liberation Sans", "Arial", ...) so a document builds on Linux, macOS and
  # Windows alike, and Typst warns about every name in the chain that is absent
  # locally even when an earlier one matched. Every other warning is left
  # visible, and the pipeline preserves typst's exit status.
  set -o pipefail
  "$TYPST" compile \
    --root .. \
    --input "data=$data" \
    ${PDF_STD:+--pdf-standard "$PDF_STD"} \
    "typst/$name.typ" "$OUT/$name.pdf" 2>&1 \
    | grep -vE 'unknown font family|^\s*[┌│]|^\s*[0-9]+ │|^\s*\^+\s*$|^\s*$' || true
  echo "    $OUT/$name.pdf"
}

build_latex() {
  echo "==> latex/policy-document.tex"
  # Its own output directory. The Typst policy template also produces
  # policy-document.pdf, and writing both to out/ meant the LaTeX build
  # silently destroyed the Typst one.
  mkdir -p "$OUT/latex"
  if have "$TECTONIC"; then
    # Tectonic downloads what it needs on first run; no TeX distribution
    # required, which is why it is the default driver here.
    (cd latex && "$TECTONIC" -X compile --outdir "../$OUT/latex" policy-document.tex)
  elif have latexmk; then
    # XeLaTeX, not pdfLaTeX: the style uses fontspec for the font stack.
    (cd latex && latexmk -xelatex -outdir="../$OUT/latex" policy-document.tex)
  else
    echo "Neither tectonic nor latexmk found." >&2
    echo "Install tectonic (single binary) or a TeX distribution with XeLaTeX." >&2
    return 1
  fi
  echo "    $OUT/latex/policy-document.pdf"
}

target="${1:-all}"
data="${2:-}"

case "$target" in
  policy)
    build_typst policy-document "${data:-../samples/policy-sample.json}"
    ;;
  report)
    build_typst business-report "${data:-../samples/report-sample.json}"
    ;;
  latex)
    build_latex
    ;;
  all)
    build_typst policy-document ../samples/policy-sample.json
    build_typst business-report ../samples/report-sample.json
    ;;
  *)
    echo "unknown target: $target (expected policy, report, latex or all)" >&2
    exit 2
    ;;
esac

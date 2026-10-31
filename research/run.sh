#!/bin/zsh
cd ${0:a:h}
for h in "$@"; do .venv/bin/python -I hypotheses/$h.py || echo "$h failed"; done

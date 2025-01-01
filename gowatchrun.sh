#!/bin/bash
echo "Running \`go run .\` in watch mode using \`gow\`.  Use ^C^C to kill \`gow\`."
gow -e go,mod,js -w .,app run .
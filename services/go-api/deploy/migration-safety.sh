#!/usr/bin/env bash

set -euo pipefail

MIGRATIONS_DIR="${1:-migrations}"
STATUS=0
MAX_VERSION_DIGITS=18

if [ ! -d "$MIGRATIONS_DIR" ]; then
    printf 'migration-safety: %s: no such directory\n' "$MIGRATIONS_DIR" >&2
    exit 1
fi

is_contract_file() {
    printf '%s' "$1" | grep -qE '^[0-9]{3,}_contract_'
}

report() {
    printf 'migration-safety: %s: %s needs expand-contract (rename the file to NNN_contract_<name>.sql once no running code needs the old shape)\n' "$1" "$2"
    STATUS=1
}

unsafe_labels() {
    local perl_script
    perl_script=$(cat <<'PERL'
        my $text = $_;

        $text =~ s{
            /\* .*? \*/
          | -- [^\n]*
          | ' (?: [^'] | '' )* '
        }{ }gxs;

        if ($text =~ /'/) {
            print STDERR "unterminated string literal\n";
            exit 3;
        }

        $text =~ s{\r}{}g;
        $text =~ s{\n}{ }g;

        my %hit;
        for my $stmt (split /;/, $text) {
            next unless $stmt =~ /\S/;

            $hit{"DROP TABLE"} = 1 if $stmt =~ /\bDROP\s+TABLE\b/i;
            $hit{"DROP COLUMN"} = 1 if $stmt =~ /\bDROP\s+COLUMN\b/i;
            $hit{"RENAME"} = 1 if $stmt =~ /\bRENAME\b/i;
            $hit{"SET NOT NULL"} = 1 if $stmt =~ /\bSET\s+NOT\s+NULL\b/i;
            $hit{"TRUNCATE"} = 1 if $stmt =~ /\bTRUNCATE\b/i;
            $hit{"DROP MATERIALIZED VIEW"} = 1 if $stmt =~ /\bDROP\s+MATERIALIZED\s+VIEW\b/i;
            $hit{"DROP TYPE"} = 1 if $stmt =~ /\bDROP\s+TYPE\b/i;
            $hit{"DROP VIEW"} = 1 if $stmt =~ /\bDROP\s+VIEW\b/i;
            $hit{"DROP SCHEMA"} = 1 if $stmt =~ /\bDROP\s+SCHEMA\b/i;
            $hit{"ALTER COLUMN ... TYPE"} = 1
                if $stmt =~ /\bALTER\s+COLUMN\b/i && $stmt =~ /\b(?:TYPE|SET\s+DATA\s+TYPE)\b/i;

            next unless $stmt =~ /\bALTER\s+TABLE\b/i;

            my @clauses;
            my $depth = 0;
            my $cur = "";
            for my $ch (split //, $stmt) {
                if ($ch eq "(") {
                    $depth++;
                    $cur .= $ch;
                } elsif ($ch eq ")") {
                    $depth--;
                    $cur .= $ch;
                } elsif ($ch eq "," && $depth == 0) {
                    push @clauses, $cur;
                    $cur = "";
                } else {
                    $cur .= $ch;
                }
            }
            push @clauses, $cur;

            for my $clause (@clauses) {
                if ($clause =~ /\bDROP\s+([A-Za-z_][A-Za-z0-9_]*)/i) {
                    my $word = uc($1);
                    $hit{"DROP COLUMN"} = 1
                        unless $word =~ /^(?:COLUMN|CONSTRAINT|INDEX|DEFAULT)$/;
                }

                next if $clause =~ /\bADD\s+CONSTRAINT\b/i;

                if ($clause =~ /\bADD\s+(?:COLUMN\s+)?[A-Za-z_][A-Za-z0-9_]*\b/i) {
                    if ($clause =~ /\bNOT\s+NULL\b/i && $clause !~ /\bDEFAULT\b/i) {
                        $hit{"ADD COLUMN ... NOT NULL without DEFAULT"} = 1;
                    }
                }
            }
        }

        for my $label (
            "DROP TABLE", "DROP COLUMN", "RENAME", "SET NOT NULL", "TRUNCATE",
            "DROP TYPE", "DROP VIEW", "DROP MATERIALIZED VIEW", "DROP SCHEMA",
            "ALTER COLUMN ... TYPE", "ADD COLUMN ... NOT NULL without DEFAULT",
        ) {
            print "$label\n" if $hit{$label};
        }
PERL
)
    perl -0777 -ne "$perl_script" "$1"
}

for file in "$MIGRATIONS_DIR"/*.sql; do
    [ -e "$file" ] || continue

    if [ ! -f "$file" ]; then
        printf 'migration-safety: %s: not a regular file\n' "$file" >&2
        STATUS=1
        continue
    fi

    base=$(basename "$file")
    version=$(printf '%s' "$base" | grep -oE '^[0-9]+' || true)
    [ ${#version} -ge 3 ] || continue
    if [ ${#version} -le "$MAX_VERSION_DIGITS" ]; then
        [ $((10#$version)) -ge 26 ] || continue
    fi
    is_contract_file "$base" && continue

    if labels=$(unsafe_labels "$file"); then
        rc=0
    else
        rc=$?
    fi

    if [ "$rc" -ne 0 ]; then
        printf 'migration-safety: %s: scan failed (exit %d)\n' "$file" "$rc" >&2
        STATUS=1
        continue
    fi

    while IFS= read -r label; do
        [ -n "$label" ] || continue
        report "$file" "$label"
    done <<<"$labels"
done

exit "$STATUS"

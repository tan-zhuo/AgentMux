A run has just finished. Assess it, and decide whether anything in it is worth
keeping as a reusable skill.

## The goal

{{.Goal}}

## What was actually done

{{range .Steps}}{{.Seq}}. [{{.Tool}}] {{.Reasoning}}
   result: {{.Result}}
{{end}}

## Outcome

{{.Outcome}}

{{if .Similar}}## Skills that already exist and look related

{{range .Similar}}- {{.Name}} (similarity {{printf "%.2f" .Score}}): {{.Description}}
{{end}}{{end}}

## Answer these in order

1. Which decisions here were right, and which were detours?
2. Is this path reusable — would it still hold on another project, another
   server, another day? A sequence that only made sense for this one situation
   is not a skill. Say which it is.
3. If a related skill already exists, the answer is to update that one, not to
   add a near-duplicate. Say which you mean.
4. Only if the answer to 2 is that it is reusable, output the skill as JSON
   matching the given schema, with a confidence between 0 and 1 saying how sure
   you are that following it again would be right. Otherwise output null.

Output the JSON object or null, and nothing else.

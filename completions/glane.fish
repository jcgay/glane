# fish completions for glane. Install: cp to ~/.config/fish/completions/
complete -c glane -f

set -l cmds import sync search serve enrich summarize update tags version

# top-level subcommands (only when none typed yet)
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a import    -d "import a Twitter archive"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a sync      -d "sync a live source"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a search    -d "full-text + semantic search"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a serve     -d "start the web UI"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a enrich    -d "fetch article bodies / embeddings"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a summarize -d "summarize + tag articles"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a update    -d "sync all configured sources + enrich"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a tags      -d "list tags"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a version   -d "print version"

# import twitter
complete -c glane -n "__fish_seen_subcommand_from import" -a twitter -d "Twitter archive"

# sync <source>
complete -c glane -n "__fish_seen_subcommand_from sync" -a "github mastodon bluesky all"

# flags
complete -c glane -n "__fish_seen_subcommand_from search" -l source -d "filter by source"
complete -c glane -n "__fish_seen_subcommand_from search" -l limit  -d "max results"
complete -c glane -n "__fish_seen_subcommand_from search" -l since  -d "on/after date (YYYY or YYYY-MM-DD)"
complete -c glane -n "__fish_seen_subcommand_from search" -l tag    -d "filter by tag"
complete -c glane -n "__fish_seen_subcommand_from serve"     -l port  -d "listen port"
complete -c glane -n "__fish_seen_subcommand_from enrich"    -l limit -d "max items this run"
complete -c glane -n "__fish_seen_subcommand_from summarize" -l limit -d "max items this run"

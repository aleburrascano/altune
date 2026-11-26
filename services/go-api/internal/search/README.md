`search` owns how search and browse results are found, merged and ranked across providers.

It owns the tables `discovery_events`, `discovery_favorites`, `discovery_search_history`, `discovery_search_history_cleared`, `entity_identity`, `unreleased_artists`, `unreleased_songs`. Only `discovery` writes them. Other modules may read them.

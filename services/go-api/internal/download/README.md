`download` owns how a saved track gets its playable audio file.

It owns the tables `acquisition_cooldowns`, `acquisition_outcomes`, `acquisition_rejections`, `track_length_fixes`. Only `acquisition` writes them. Other modules may read them.

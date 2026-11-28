`download` owns how a saved track gets its playable audio file.

It owns the tables `acquisition_cooldowns`, `acquisition_jobs`, `acquisition_outcomes`, `acquisition_rejections`, `acquisition_upgrade_state`. Only `download` writes them. Other modules may read them.

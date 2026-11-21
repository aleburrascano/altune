`acquisition` owns how a saved track gets its playable audio file.

It owns the tables `acquisition_cooldowns`, `acquisition_outcomes`, `acquisition_rejections`. Other modules reach them only through `acquisition`.

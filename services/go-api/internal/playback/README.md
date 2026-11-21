`playback` owns the queue snapshot a user resumes playback from.

It owns the tables `playback_queue_state`. Other modules reach them only through `playback`.

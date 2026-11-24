`acquisitioncatalog` owns how acquisition's ports are served by catalog.

It translates only: each type here implements one acquisition port on a catalog service, so neither feature imports the other.

- `NewStoredAudioReader` serves acquisition's stored-audio reader from catalog's audio store, copying a stored file into a working directory so acquisition can check it again.

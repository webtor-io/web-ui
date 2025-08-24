-- Revert library.name to NULL where it was populated from torrent_resource.name
UPDATE public.library l
SET name = NULL 
FROM public.torrent_resource tr
WHERE l.resource_id = tr.resource_id 
  AND l.name = tr.name;
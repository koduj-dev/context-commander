DROP TRIGGER IF EXISTS phases_storage ON phases;
DROP TRIGGER IF EXISTS plans_storage ON plans;
DROP FUNCTION IF EXISTS cc_plan_text_storage();
DROP FUNCTION IF EXISTS cc_prune_presence();

DROP TABLE IF EXISTS work_presence;
DROP TABLE IF EXISTS plan_item_effort;
DROP TABLE IF EXISTS plan_item_transitions;
DROP TABLE IF EXISTS plan_items;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS phases;

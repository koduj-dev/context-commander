DROP TRIGGER IF EXISTS agent_messages_storage ON agent_messages;
DROP FUNCTION IF EXISTS cc_prune_agent_messages();
DROP TABLE IF EXISTS agent_message_deliveries;
DROP TABLE IF EXISTS agent_messages;

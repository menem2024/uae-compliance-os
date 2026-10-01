from ai.gen.compliance.v1 import agents_pb2, documents_pb2


def test_agent_messages_have_contract_field_numbers():
    f = agents_pb2.AgentStepEvent.DESCRIPTOR.fields_by_name
    assert (f["seq"].number, f["usage"].number, f["message_args"].number, f["client_company_id"].number) == (4, 14, 16, 20)
    assert agents_pb2.AgentRunStatus.Value("AGENT_RUN_STATUS_BUDGET_EXCEEDED") == 4
    assert agents_pb2.AgentTaskCompleted.DESCRIPTOR.fields_by_name["subject_id"].number == 10


def test_document_messages_have_contract_field_numbers():
    f = documents_pb2.DocumentUploaded.DESCRIPTOR.fields_by_name
    assert (f["candidates"].number, f["reprocess_nonce"].number) == (9, 10)
    e = documents_pb2.ExtractedInvoice.DESCRIPTOR.fields_by_name
    assert (e["invoice"].message_type.full_name, e["verdict"].number) == ("compliance.v1.Invoice", 6)
    assert documents_pb2.DocumentExtracted.DESCRIPTOR.fields_by_name["review_reasons"].number == 11

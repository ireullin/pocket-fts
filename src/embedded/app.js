(() => {
    const state = {
        API_BASE: '',
        collections: [],
        currentCollection: null,
        currentPage: 1,
        limit: 20,
        content: null,
        schema: null,
        conditionCounter: 0,
        showingQuery: false,
    };

    const refs = {};

    document.addEventListener('DOMContentLoaded', () => {
        cacheElements();
        bindEvents();
        updateQueryPreview();
        loadCollections();
        checkServerStatus();
    });

    function cacheElements() {
        refs.toast = document.getElementById('toast');
        refs.statusIndicator = document.getElementById('status-indicator');
        refs.statusText = document.getElementById('status-text');
        refs.collectionList = document.getElementById('collection-list');
        refs.openCreateCollection = document.getElementById('open-create-collection');
        refs.openAddDocument = document.getElementById('open-add-document');
        refs.resultLimit = document.getElementById('result-limit');
        refs.resultOffset = document.getElementById('result-offset');
        refs.queryBuilder = document.getElementById('query-builder');
        refs.addCondition = document.getElementById('add-condition');
        refs.executeQuery = document.getElementById('execute-query');
        refs.clearQuery = document.getElementById('clear-query');
        refs.contentTitle = document.getElementById('content-title');
        refs.collectionInfo = document.getElementById('collection-info');
        refs.contentTableContainer = document.getElementById('content-table-container');
        refs.pagination = document.getElementById('pagination');
        refs.queryPreview = document.getElementById('query-json-preview');
        refs.copyQueryJson = document.getElementById('copy-query-json');
        refs.modalAddDocument = document.getElementById('modal-add-document');
        refs.modalEditDocument = document.getElementById('modal-edit-document');
        refs.modalCreateCollection = document.getElementById('modal-create-collection');
        refs.addFields = document.getElementById('add-fields');
        refs.editFields = document.getElementById('edit-fields');
        refs.addDocumentForm = document.getElementById('add-document-form');
        refs.editDocumentForm = document.getElementById('edit-document-form');
        refs.createCollectionForm = document.getElementById('create-collection-form');
        refs.addFieldRowButton = document.getElementById('add-field-row');
        refs.fieldsContainer = document.getElementById('fields-container');
        resetCreateCollectionForm();
    }

    function bindEvents() {
        refs.collectionList.addEventListener('click', handleCollectionClick);
        refs.openCreateCollection.addEventListener('click', () => toggleModal(refs.modalCreateCollection, true));
        refs.openAddDocument.addEventListener('click', handleOpenAddDocument);
        refs.addCondition.addEventListener('click', () => addCondition());
        refs.executeQuery.addEventListener('click', executeQuery);
        refs.clearQuery.addEventListener('click', clearQuery);
        refs.copyQueryJson.addEventListener('click', copyQueryToClipboard);
        refs.queryBuilder.addEventListener('change', updateQueryPreview);
        refs.queryBuilder.addEventListener('input', updateQueryPreview);
        refs.queryBuilder.addEventListener('click', handleConditionAction);
        refs.resultLimit.addEventListener('input', updateQueryPreview);
        refs.resultOffset.addEventListener('input', updateQueryPreview);
        refs.contentTableContainer.addEventListener('click', handleTableAction);
        refs.addDocumentForm.addEventListener('submit', submitAddDocument);
        refs.editDocumentForm.addEventListener('submit', submitEditDocument);
        refs.createCollectionForm.addEventListener('submit', submitCreateCollection);
        refs.addFieldRowButton.addEventListener('click', () => addFieldRow());
        document.body.addEventListener('click', handleGlobalClick, true);
    }

    function handleGlobalClick(event) {
        const target = event.target.closest('[data-close]');
        if (target) {
            const id = target.getAttribute('data-close');
            const modal = document.getElementById(id);
            if (modal) {
                toggleModal(modal, false);
            }
            return;
        }

        if (event.target.classList.contains('modal')) {
            toggleModal(event.target, false);
        }
    }

    async function loadCollections() {
        try {
            const result = await apiCall('/collections/list');
            state.collections = result.collections || [];
            renderCollections();
            showToast(`Loaded ${state.collections.length} collections`);
        } catch (error) {
            renderCollections([]);
            showToast(`Failed to load collections: ${error.message}`, true);
        } finally {
            checkServerStatus();
        }
    }

    function renderCollections(list = state.collections) {
        if (!list.length) {
            refs.collectionList.innerHTML = '<li class="collection-list__item collection-list__item--empty">No data</li>';
            return;
        }

        refs.collectionList.innerHTML = list.map(collection => {
            const activeClass = collection.name === state.currentCollection ? 'collection-list__item--active' : '';
            const count = collection.document_count ?? '–';
            return `
                <li class="collection-list__item ${activeClass}" data-collection="${collection.name}">
                    <button type="button" class="delete-icon" data-action="delete-collection" data-collection="${collection.name}" title="Delete collection">🗑️</button>
                    <span>${collection.name}</span>
                    <span class="collection-list__meta">${count}</span>
                </li>
            `;
        }).join('');

        if (!state.currentCollection) {
            const first = list[0]?.name;
            if (first) {
                selectCollection(first);
            }
        }
    }

    function handleCollectionClick(event) {
        // Check if delete button was clicked
        const deleteBtn = event.target.closest('[data-action="delete-collection"]');
        if (deleteBtn) {
            event.stopPropagation();
            const collectionName = deleteBtn.getAttribute('data-collection');
            deleteCollectionByName(collectionName);
            return;
        }

        // Handle collection selection
        const item = event.target.closest('[data-collection]');
        if (!item) return;
        const name = item.getAttribute('data-collection');
        selectCollection(name);
    }

    function selectCollection(name) {
        if (!name) return;
        state.currentCollection = name;
        state.currentPage = 1;
        state.showingQuery = false;
        renderCollections();
        refs.openAddDocument.disabled = false;
        loadCollectionContent(name, 1);
        showToast(`Selected collection: ${name}`);
    }

    async function loadCollectionContent(collectionName = state.currentCollection, page = state.currentPage) {
        if (!collectionName) return;
        try {
            const url = `/collections/content?collection=${encodeURIComponent(collectionName)}&page=${page}&limit=${state.limit}`;
            const result = await apiCall(url);
            state.content = result;
            state.currentPage = result.pagination?.page || page;
            state.limit = result.pagination?.limit || state.limit;
            state.schema = parseSchema(result.schema, collectionName);
            renderCollectionContent(result);
        } catch (error) {
            state.content = null;
            state.schema = null;
            renderEmptyContent(error.message);
            showToast(`Failed to load content: ${error.message}`, true);
        }
    }

    function parseSchema(schemaText, collectionName) {
        if (!schemaText) return null;
        try {
            return JSON.parse(schemaText);
        } catch (error) {
            console.warn('Unable to parse schema for', collectionName, error);
            return null;
        }
    }

    function renderCollectionContent(content) {
        const columns = content.columns || inferColumns(content.records);
        const pagination = content.pagination || { page: 1, total_pages: 1, total_count: content.records?.length || 0 };
        const collectionName = content.collection || state.currentCollection || 'Untitled';

        refs.contentTitle.textContent = `${collectionName} - Content`;
        refs.collectionInfo.textContent = `${pagination.total_count} records, page ${pagination.page}/${pagination.total_pages}`;
        refs.openAddDocument.disabled = false;

        renderTable(columns, content.records || []);
        renderPagination(pagination);
    }

    function renderEmptyContent(message) {
        refs.contentTitle.textContent = 'Collection Content';
        refs.collectionInfo.textContent = message || 'Please select a collection';
        refs.openAddDocument.disabled = !state.currentCollection;
        refs.contentTableContainer.innerHTML = `
            <div class="placeholder">
                <h3>${message ? 'Load failed' : 'No collection selected'}</h3>
                <p>${message || 'Please select a collection from the left'}</p>
            </div>
        `;
        refs.pagination.innerHTML = '';
    }

    function renderTable(columns, records) {
        if (!records.length) {
            refs.contentTableContainer.innerHTML = `
                <div class="placeholder">
                    <h3>沒有資料</h3>
                    <p>目前沒有可以顯示的文檔。</p>
                </div>
            `;
            return;
        }

        const hasScoreColumn = records.some(record => record && Object.prototype.hasOwnProperty.call(record, '_score'));
        let displayColumns = Array.isArray(columns) ? [...columns] : [];
        if (!displayColumns.length) {
            displayColumns = inferColumns(records);
        }
        if (hasScoreColumn) {
            displayColumns = displayColumns.filter(col => col !== '_score');
            displayColumns.unshift('_score');
        }

        const primaryKey = state.schema?.primary_key
            || displayColumns.find(col => col && !col.startsWith('_'))
            || displayColumns[0];
        const thead = `
            <thead>
                <tr>
                    ${displayColumns.map(col => `<th>${col === '_score' ? 'score' : col}</th>`).join('')}
                    <th style="width: 80px;">actions</th>
                </tr>
            </thead>
        `;

        const tbody = `
            <tbody>
                ${records.map((record, index) => `
                    <tr data-row="${index}" data-id="${record[primaryKey] ?? ''}">
                        ${displayColumns.map(col => `
                            <td>
                                <div class="cell-content" title="Click to expand" data-action="toggle-cell">
                                    ${formatCell(record[col], col)}
                                </div>
                            </td>
                        `).join('')}
                        <td>
                            <div class="table-actions">
                                <a href="#" data-action="edit" data-index="${index}">Edit</a>
                                <a href="#" data-action="delete" data-id="${record[primaryKey] ?? ''}">Delete</a>
                            </div>
                        </td>
                    </tr>
                `).join('')}
            </tbody>
        `;

        refs.contentTableContainer.innerHTML = `<div class="table-wrapper"><table class="data-table">${thead}${tbody}</table></div>`;
    }

    function renderPagination(pagination) {
        if (!pagination || pagination.total_pages <= 1) {
            refs.pagination.innerHTML = '';
            return;
        }

        refs.pagination.innerHTML = `
            <span>Page ${pagination.page} / ${pagination.total_pages}, Total: ${pagination.total_count}</span>
            <div class="pagination__controls">
                <button type="button" class="ghost" data-page="prev" ${pagination.page <= 1 ? 'disabled' : ''}>Previous</button>
                <button type="button" class="ghost" data-page="next" ${pagination.page >= pagination.total_pages ? 'disabled' : ''}>Next</button>
            </div>
        `;

        refs.pagination.querySelectorAll('button').forEach(btn => {
            btn.addEventListener('click', handlePaginationClick);
        });
    }

    function handlePaginationClick(event) {
        const { page } = event.currentTarget.dataset;
        const pagination = state.content?.pagination;
        if (!pagination) return;
        let targetPage = pagination.page;
        if (page === 'prev' && pagination.page > 1) targetPage = pagination.page - 1;
        if (page === 'next' && pagination.page < pagination.total_pages) targetPage = pagination.page + 1;
        if (targetPage === pagination.page) return;
        loadCollectionContent(state.currentCollection, targetPage);
    }

    function handleTableAction(event) {
        const actionButton = event.target.closest('[data-action]');
        if (!actionButton) return;
        const action = actionButton.getAttribute('data-action');

        if (action === 'toggle-cell') {
            event.target.classList.toggle('is-expanded');
            return;
        }

        if (action === 'edit') {
            event.preventDefault();
            const index = Number(actionButton.getAttribute('data-index'));
            const record = state.content?.records?.[index];
            if (record) openEditDocument(record);
            return;
        }

        if (action === 'delete') {
            event.preventDefault();
            const id = actionButton.getAttribute('data-id');
            deleteRecord(id);
        }
    }


    async function handleConditionAction(event) {
        const removeBtn = event.target.closest('[data-role="remove-condition"]');
        if (removeBtn) {
            const condition = removeBtn.closest('.query-condition');
            condition?.remove();
            updateSearchOperatorVisibility();
            updateQueryPreview();
            return;
        }

        const typeSelect = event.target.closest('[data-role="condition-type"]');
        if (typeSelect) {
            const condition = typeSelect.closest('.query-condition');
            await renderConditionFields(condition, typeSelect.value);
            updateSearchOperatorVisibility();
            updateQueryPreview();
        }
    }

    async function addCondition(type = 'search') {
        state.conditionCounter += 1;
        const id = `condition-${state.conditionCounter}`;
        const element = document.createElement('div');
        element.className = 'query-condition';
        element.dataset.condition = id;
        element.innerHTML = `
            <select data-role="condition-type">
                <option value="search" ${type === 'search' ? 'selected' : ''}>search</option>
                <option value="sql" ${type === 'sql' ? 'selected' : ''}>sql</option>
            </select>
            <div class="query-condition__fields"></div>
            <button type="button" class="ghost" data-role="remove-condition" title="Remove">✕</button>
        `;
        refs.queryBuilder.appendChild(element);
        await renderConditionFields(element, type);
        updateSearchOperatorVisibility();
        updateQueryPreview();
    }

    async function renderConditionFields(condition, type) {
        const fieldsContainer = condition.querySelector('.query-condition__fields');
        if (!fieldsContainer) return;

        if (type === 'search') {
            fieldsContainer.innerHTML = `
                <select data-role="search-operator">
                    <option value="AND">AND</option>
                    <option value="OR">OR</option>
                    <option value="NOT">NOT</option>
                </select>
                <select data-role="search-fields"></select>
                <input type="text" placeholder="Keyword" data-role="search-term">
            `;
            await populateSearchFields(fieldsContainer.querySelector('[data-role="search-fields"]'));
            updateSearchOperatorVisibility();
        } else {
            fieldsContainer.innerHTML = `
                <select data-role="sql-field"></select>
                <select data-role="sql-operator">
                    <option value="=">=</option>
                    <option value="!=">!=</option>
                    <option value=">">></option>
                    <option value=">=">>=</option>
                    <option value="<"><</option>
                    <option value="<="><=</option>
                    <option value="LIKE">LIKE</option>
                </select>
                <input type="text" placeholder="Value" data-role="sql-value">
            `;
            await populateSqlFields(fieldsContainer.querySelector('[data-role="sql-field"]'));
        }
    }

    async function populateSearchFields(selectElement) {
        const fields = await fetchSchemaFields('text');
        selectElement.innerHTML = '<option value="">All fields</option>' +
            fields.map(field => `<option value="${field.name}">${field.name} (${field.type})</option>`).join('');
    }

    async function populateSqlFields(selectElement) {
        const fields = await fetchSchemaFields();
        selectElement.innerHTML = '<option value="">Select field</option>' +
            fields.map(field => `<option value="${field.name}">${field.name} (${field.type})</option>`).join('');
    }

    function updateSearchOperatorVisibility() {
        if (!refs.queryBuilder) return;
        const searchConditions = Array.from(refs.queryBuilder.querySelectorAll('.query-condition'))
            .filter(condition => condition.querySelector('[data-role="condition-type"]')?.value === 'search');

        searchConditions.forEach((condition, index) => {
            const operatorSelect = condition.querySelector('[data-role="search-operator"]');
            if (!operatorSelect) return;

            if (index === 0) {
                operatorSelect.style.display = 'none';
                operatorSelect.value = 'AND';
            } else {
                operatorSelect.style.display = '';
            }
        });
    }

    async function fetchSchemaFields(filterType) {
        // Use the currently selected collection
        const collection = state.currentCollection;
        if (!collection) return [];

        // Load schema if not available
        if (!state.schema) {
            try {
                const url = `/collections/content?collection=${encodeURIComponent(collection)}&page=1&limit=1`;
                const result = await apiCall(url);
                state.schema = parseSchema(result.schema, collection);
            } catch (error) {
                console.warn('Failed to load schema:', error);
                return [];
            }
        }

        const schema = state.schema;
        if (!schema?.fields) return [];
        return schema.fields.filter(field => !filterType || field.type === filterType);
    }

    async function refreshConditionOptions(condition) {
        const type = condition.querySelector('[data-role="condition-type"]').value;
        await renderConditionFields(condition, type);
    }

    function updateQueryPreview() {
        const collection = state.currentCollection || '';
        const limit = Number(refs.resultLimit.value) || 20;
        const offset = Number(refs.resultOffset.value) || 0;
        const conditions = Array.from(refs.queryBuilder.querySelectorAll('.query-condition'));
        const searchConditions = [];
        const sqlTuples = [];

        conditions.forEach((condition, index) => {
            const type = condition.querySelector('[data-role="condition-type"]')?.value;
            if (type === 'search') {
                const fieldValue = condition.querySelector('[data-role="search-fields"]')?.value?.trim() || '';
                const field = fieldValue.toUpperCase() === 'ALL' ? '' : fieldValue;
                const termInput = condition.querySelector('[data-role="search-term"]');
                const operatorSelect = condition.querySelector('[data-role="search-operator"]');
                const term = termInput ? termInput.value.trim() : '';
                if (!term) return;
                const operator = normalizeBooleanOperator(operatorSelect ? operatorSelect.value : 'AND');
                searchConditions.push({ index, field, term, operator });
            } else if (type === 'sql') {
                const field = condition.querySelector('[data-role="sql-field"]')?.value;
                const operator = condition.querySelector('[data-role="sql-operator"]')?.value || '=';
                const valueRaw = condition.querySelector('[data-role="sql-value"]')?.value.trim() || '';
                if (!field || !valueRaw) return;
                const value = operator === 'LIKE' ? `%${valueRaw}%` : valueRaw;
                sqlTuples.push([field, operator, value]);
            }
        });

        const searchPart = buildSearchQueryPart(searchConditions);

        const request = {
            collection,
            limit,
            offset,
            order_by: [{ field: '_score', direction: 'desc' }],
        };

        if (searchPart) {
            request.search = { term: searchPart.term };
        }

        if (sqlTuples.length) {
            request.sql = sqlTuples;
        }

        // When no filters, fall back to SQL-only full scan
        if (!searchPart && !sqlTuples.length) {
            request.sql = [];
        }

        refs.queryPreview.textContent = JSON.stringify(request, null, 2);
    }

    function normalizeBooleanOperator(value) {
        const upper = value ? value.toUpperCase() : '';
        if (upper === 'OR') return 'OR';
        if (upper === 'NOT') return 'NOT';
        return 'AND';
    }

    function buildSearchQueryPart(conditions) {
        if (!Array.isArray(conditions) || !conditions.length) return null;

        const parts = [];

        conditions.forEach(condition => {
            let termText = condition.term.trim();
            if (!termText) return;

            let isNegated = false;
            if (/^NOT\s+/i.test(termText)) {
                isNegated = true;
                termText = termText.replace(/^NOT\s+/i, '').trim();
            }

            if (!termText) return;

            let formatted = condition.field ? `${condition.field}:${termText}` : termText;
            if (isNegated) {
                formatted = `NOT ${formatted}`;
            }

            const connector = condition.operator || 'AND';
            if (!parts.length) {
                parts.push(formatted);
                return;
            }

            if (connector === 'NOT') {
                const withoutLeadingNot = formatted.startsWith('NOT ')
                    ? formatted.slice(4).trim()
                    : formatted;
                if (!withoutLeadingNot) {
                    return;
                }
                parts.push(`NOT ${withoutLeadingNot}`);
                return;
            }

            parts.push(`${connector} ${formatted}`);
        });

        if (!parts.length) {
            return null;
        }

        return {
            term: parts.join(' '),
        };
    }

    async function executeQuery() {
        updateQueryPreview();
        try {
            const payload = JSON.parse(refs.queryPreview.textContent);
            if (!payload.collection) {
                showToast('Please select a collection', true);
                return;
            }
            const result = await apiCall('/query', 'POST', payload);
            displayQueryResult(payload.collection, result);
            showToast('Advanced query completed');
        } catch (error) {
            showToast(`Query failed: ${error.message}`, true);
        }
    }

    function displayQueryResult(collectionName, result) {
        state.showingQuery = true;
        // 後端可能直接返回陣列，或返回包含 records 的物件
        const records = Array.isArray(result) ? result : (Array.isArray(result.records) ? result.records : (result.records ? [result.records] : []));
        const columns = result.columns || inferColumns(records);
        const total = result.pagination?.total_count ?? records.length;
        const pagination = result.pagination || { page: 1, total_pages: 1, total_count: total };

        refs.contentTitle.textContent = `Query Results - ${collectionName}`;
        refs.collectionInfo.textContent = `${pagination.total_count} matching records`;
        refs.openAddDocument.disabled = true;

        if (!records.length) {
            refs.contentTableContainer.innerHTML = `
                <div class="placeholder">
                    <h3>No Data</h3>
                    <p>No matching results.</p>
                </div>
            `;
            refs.pagination.innerHTML = '';
            return;
        }

        renderTable(columns, records);
        refs.pagination.innerHTML = '';
    }

    function clearQuery() {
        refs.queryBuilder.innerHTML = '';
        state.conditionCounter = 0;
        refs.resultLimit.value = 20;
        refs.resultOffset.value = 0;
        updateSearchOperatorVisibility();
        updateQueryPreview();
        if (state.currentCollection) {
            loadCollectionContent(state.currentCollection, 1);
        } else {
            renderEmptyContent('Please select a collection');
        }
        showToast('Query cleared');
    }

    async function copyQueryToClipboard() {
        const jsonText = refs.queryPreview.textContent;
        try {
            await navigator.clipboard.writeText(jsonText);
            showToast('Query JSON copied to clipboard');
        } catch (error) {
            // Fallback for older browsers
            const textArea = document.createElement('textarea');
            textArea.value = jsonText;
            textArea.style.position = 'fixed';
            textArea.style.left = '-9999px';
            document.body.appendChild(textArea);
            textArea.select();
            try {
                document.execCommand('copy');
                showToast('Query JSON copied to clipboard');
            } catch (err) {
                showToast('Failed to copy to clipboard', true);
            }
            document.body.removeChild(textArea);
        }
    }

    function formatCell(value, columnKey) {
        if (value === null || value === undefined) return '<span style="color:#888">null</span>';

        const isScoreColumn = columnKey === '_score';
        let displayValue = value;
        let fullValue = value;

        if (isScoreColumn) {
            const numeric = Number(value);
            if (!Number.isNaN(numeric)) {
                displayValue = numeric.toFixed(3);
                fullValue = numeric.toString();
            } else {
                displayValue = String(value);
                fullValue = displayValue;
            }
        } else if (typeof value === 'string') {
            fullValue = value;
            if (value.length > 80) {
                displayValue = `${value.slice(0, 80)}…`;
            } else {
                displayValue = value;
            }
        } else {
            const strValue = String(value);
            fullValue = strValue;
            if (strValue.length > 80) {
                displayValue = `${strValue.slice(0, 80)}…`;
            } else {
                displayValue = strValue;
            }
        }

        const safeDisplay = escapeHtml(String(displayValue));
        const safeFull = escapeHtml(String(fullValue));
        if (safeDisplay === safeFull) {
            return safeDisplay;
        }

        return `<span title="${safeFull}">${safeDisplay}</span>`;
    }

    function inferColumns(records) {
        if (!records || !records.length) return [];
        return Object.keys(records[0]).filter(key => !key.startsWith('_'));
    }

    function escapeHtml(str) {
        return str
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function toggleModal(modal, visible) {
        if (!modal) return;
        modal.setAttribute('aria-hidden', visible ? 'false' : 'true');
    }

    function handleOpenAddDocument() {
        if (!state.currentCollection) {
            showToast('Please select a collection', true);
            return;
        }
        if (!state.schema) {
            showToast('Schema not available', true);
            return;
        }
        buildDocumentFields(refs.addFields, state.schema.fields, {});
        toggleModal(refs.modalAddDocument, true);
    }

    function openEditDocument(record) {
        if (!state.schema) {
            showToast('Schema not available', true);
            return;
        }
        buildDocumentFields(refs.editFields, state.schema.fields, record, true, state.schema.primary_key);
        toggleModal(refs.modalEditDocument, true);
    }

    function buildDocumentFields(container, fields, record = {}, isEdit = false, primaryKey) {
        container.innerHTML = '';
        if (!Array.isArray(fields) || !fields.length) {
            container.innerHTML = '<p class="section-subtitle">Missing schema definition。</p>';
            return;
        }

        fields.forEach(field => {
            const wrapper = document.createElement('label');
            wrapper.className = 'form-field';
            const isPrimary = primaryKey && field.name === primaryKey;
            wrapper.innerHTML = `
                <span>${field.name} (${field.type})${isPrimary ? ' *' : ''}</span>
            `;
            const input = createFieldInput(field, record[field.name]);
            input.name = field.name;
            input.required = true;
            if (isEdit && isPrimary) {
                input.readOnly = true;
                input.classList.add('is-readonly');
                input.required = false;
            }
            wrapper.appendChild(input);
            container.appendChild(wrapper);
        });
    }

    function createFieldInput(field, value) {
        let element;
        if (field.type === 'text' && (field.name.includes('content') || field.name.includes('description'))) {
            element = document.createElement('textarea');
        } else if (field.type === 'integer' || field.type === 'real') {
            element = document.createElement('input');
            element.type = 'number';
            element.step = field.type === 'integer' ? '1' : 'any';
        } else {
            element = document.createElement('input');
            element.type = 'text';
        }
        element.value = value ?? '';
        return element;
    }

    async function submitAddDocument(event) {
        event.preventDefault();
        try {
            const documentData = collectDocumentData(refs.addFields, state.schema);
            await apiCall('/documents/upsert', 'POST', {
                collection: state.currentCollection,
                document: documentData,
            });
            toggleModal(refs.modalAddDocument, false);
            showToast('Document added successfully');
            loadCollectionContent(state.currentCollection, 1);
        } catch (error) {
            showToast(`Failed to add document: ${error.message}`, true);
        }
    }

    async function submitEditDocument(event) {
        event.preventDefault();
        try {
            const documentData = collectDocumentData(refs.editFields, state.schema, true);
            await apiCall('/documents/upsert', 'POST', {
                collection: state.currentCollection,
                document: documentData,
            });
            toggleModal(refs.modalEditDocument, false);
            showToast('Document updated successfully');
            loadCollectionContent(state.currentCollection, state.currentPage);
        } catch (error) {
            showToast(`Failed to update document: ${error.message}`, true);
        }
    }

    function collectDocumentData(container, schema, preservePrimary) {
        if (!schema?.fields) throw new Error('Missing schema');
        const data = {};
        const primaryKey = schema.primary_key;

        schema.fields.forEach(field => {
            const input = container.querySelector(`[name="${field.name}"]`);
            if (!input) return;
            const rawValue = input.value;
            const value = rawValue.trim();

            if (!value) {
                if (preservePrimary && field.name === primaryKey) {
                    data[field.name] = rawValue;
                    return;
                }
                throw new Error(`${field.name} 不能為空`);
            }

            if (field.type === 'integer') {
                const parsed = parseInt(value, 10);
                if (Number.isNaN(parsed)) throw new Error(`${field.name} 必須是整數`);
                data[field.name] = parsed;
                return;
            }
            if (field.type === 'real') {
                const parsed = parseFloat(value);
                if (Number.isNaN(parsed)) throw new Error(`${field.name} 必須是數字`);
                data[field.name] = parsed;
                return;
            }
            data[field.name] = value;
        });

        return data;
    }

    async function deleteRecord(id) {
        if (!state.currentCollection || !id) {
            showToast('Missing delete information', true);
            return;
        }

        const confirmDelete = window.confirm(`Delete document with ID ${id}?`);
        if (!confirmDelete) return;

        try {
            await apiCall('/documents/delete', 'POST', {
                collection: state.currentCollection,
                id,
            });
            showToast('Document deleted successfully');
            loadCollectionContent(state.currentCollection, state.currentPage);
        } catch (error) {
            showToast(`Delete failed: ${error.message}`, true);
        }
    }

    async function submitCreateCollection(event) {
        event.preventDefault();
        try {
            const name = document.getElementById('new-collection-name').value.trim();
            const primaryKey = document.getElementById('new-primary-key').value.trim();
            const stemming = document.getElementById('fts-stemming').checked;

            if (!name || !primaryKey) {
                showToast('Please fill required fields', true);
                return;
            }

            const fields = Array.from(refs.fieldsContainer.querySelectorAll('.field-row')).map(row => {
                const fieldName = row.querySelector('[data-field="name"]').value.trim();
                const fieldType = row.querySelector('[data-field="type"]').value;
                const fieldWeight = row.querySelector('[data-field="weight"]').value;
                const indexed = row.querySelector('[data-field="indexed"]').checked;

                if (!fieldName) throw new Error('Field name cannot be empty');

                const field = { name: fieldName, type: fieldType, indexed };
                if (fieldWeight) {
                    const weightVal = parseFloat(fieldWeight);
                    if (!Number.isNaN(weightVal)) field.weight = weightVal;
                }
                return field;
            });

            if (!fields.length) {
                showToast('At least one field required', true);
                return;
            }

            const schema = {
                name,
                primary_key: primaryKey,
                fts: { stemming },
                fields,
            };

            await apiCall('/collections/create', 'POST', schema);
            toggleModal(refs.modalCreateCollection, false);
            showToast('Collection created successfully');
            loadCollections();
        } catch (error) {
            showToast(`Create failed: ${error.message}`, true);
        }
    }

    async function deleteCollectionByName(collectionName) {
        if (!collectionName) return;

        const confirmDelete = window.confirm(`Delete collection "${collectionName}"?`);
        if (!confirmDelete) return;

        try {
            await apiCall('/collections/delete', 'POST', { name: collectionName });
            showToast('Collection deleted successfully');

            // Clear state if deleted collection was selected
            if (state.currentCollection === collectionName) {
                state.currentCollection = null;
                state.content = null;
                state.schema = null;
                refs.openAddDocument.disabled = true;
                renderEmptyContent('Please select a collection');
            }

            loadCollections();
        } catch (error) {
            showToast(`Delete failed: ${error.message}`, true);
        }
    }

    function addFieldRow(name = '', type = 'text', weight = '', indexed = true) {
        const row = document.createElement('div');
        row.className = 'field-row';
        row.innerHTML = `
            <input type="text" placeholder="Field name" value="${name}" data-field="name" required>
            <select data-field="type">
                <option value="text" ${type === 'text' ? 'selected' : ''}>text</option>
                <option value="integer" ${type === 'integer' ? 'selected' : ''}>integer</option>
                <option value="real" ${type === 'real' ? 'selected' : ''}>real</option>
            </select>
            <input type="number" step="0.1" min="0" placeholder="Weight" value="${weight}" data-field="weight">
            <label class="form-field form-field--inline" style="margin:0;">
                <input type="checkbox" data-field="indexed" ${indexed ? 'checked' : ''}>
                <span>Index</span>
            </label>
            <button type="button" class="ghost" title="Remove" aria-label="Remove field">✕</button>
        `;
        row.querySelector('button').addEventListener('click', () => row.remove());
        refs.fieldsContainer.appendChild(row);
    }

    function resetCreateCollectionForm() {
        refs.fieldsContainer.innerHTML = '';
        addFieldRow('id', 'text', '', true);
        addFieldRow('title', 'text', '2', true);
        addFieldRow('content', 'text', '1', true);
        addFieldRow('created_at', 'integer', '', true);
    }

    async function apiCall(endpoint, method = 'GET', data) {
        const options = {
            method,
            headers: {
                'Content-Type': 'application/json',
                'Cache-Control': 'no-cache',
            },
        };

        if (data) {
            options.body = JSON.stringify(data);
        }

        const separator = endpoint.includes('?') ? '&' : '?';
        const response = await fetch(`${state.API_BASE}${endpoint}${separator}_t=${Date.now()}`, options);
        const text = await response.text();

        let json;
        try {
            json = JSON.parse(text);
        } catch (error) {
            json = text;
        }

        if (!response.ok) {
            const message = json?.error || json?.message || response.statusText;
            throw new Error(message);
        }

        return json;
    }

    function showToast(message, isError = false) {
        if (!refs.toast) return;
        refs.toast.textContent = message;
        refs.toast.classList.toggle('toast--visible', true);
        refs.toast.style.background = isError ? '#2f2f2f' : '#1f1f1f';
        setTimeout(() => {
            refs.toast.classList.remove('toast--visible');
        }, 3200);
    }

    async function checkServerStatus() {
        try {
            const response = await fetch('/');
            if (response.ok) {
                refs.statusIndicator.style.background = '#d0d0d0';
                refs.statusText.textContent = 'Running';
            } else {
                throw new Error('Status error');
            }
        } catch (error) {
            refs.statusIndicator.style.background = '#616161';
            refs.statusText.textContent = 'Connection error';
        }
    }


    function toggleModal(modal, visible) {
        if (!modal) return;
        const isOpening = visible && modal.getAttribute('aria-hidden') !== 'false';
        modal.setAttribute('aria-hidden', visible ? 'false' : 'true');
        if (isOpening && modal === refs.modalCreateCollection) {
            resetCreateCollectionForm();
            document.getElementById('new-collection-name').value = '';
            document.getElementById('new-primary-key').value = 'id';
            document.getElementById('fts-stemming').checked = true;
        }
    }
})();

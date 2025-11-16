// Base URL for API calls
const API_BASE = window.location.origin;

// Tab switching functionality
document.querySelectorAll('.tab-button').forEach(button => {
    button.addEventListener('click', () => {
        const tabName = button.dataset.tab;

        // Remove active class from all tabs and buttons
        document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(content => content.classList.remove('active'));

        // Add active class to clicked button and corresponding content
        button.classList.add('active');
        document.getElementById(tabName).classList.add('active');
    });
});

// Utility function to show notifications
function showNotification(message, type = 'info') {
    const notification = document.getElementById('notification');
    notification.textContent = message;
    notification.className = `notification ${type} show`;

    setTimeout(() => {
        notification.classList.remove('show');
    }, 3000);
}

// Utility function to make API calls
async function apiCall(endpoint, method = 'GET', data = null) {
    try {
        const options = {
            method,
            headers: {
                'Content-Type': 'application/json',
            }
        };

        if (data) {
            options.body = JSON.stringify(data);
        }

        const response = await fetch(`${API_BASE}${endpoint}`, options);
        const result = await response.json();

        if (!response.ok) {
            throw new Error(result.error?.message || `HTTP error! status: ${response.status}`);
        }

        return result;
    } catch (error) {
        console.error('API call failed:', error);
        throw error;
    }
}

// Health check functionality
async function checkHealth() {
    const statusDiv = document.getElementById('health-status');

    try {
        const result = await apiCall('/health');
        statusDiv.className = 'status success';
        statusDiv.textContent = `Статус: ${result.status}`;
        showNotification('Система работает нормально', 'success');
    } catch (error) {
        statusDiv.className = 'status error';
        statusDiv.textContent = `Ошибка: ${error.message}`;
        showNotification('Ошибка проверки здоровья системы', 'error');
    }
}

// Assignment stats
async function loadAssignmentStats() {
    const statsPre = document.getElementById("assignment-stats");
    if (!statsPre) {
        return;
    }
    statsPre.className = "status";
    statsPre.textContent = "Загрузка...";

    try {
        const result = await apiCall("/stats/assignments");
        statsPre.className = "status success";
        statsPre.textContent = JSON.stringify(result, null, 2);
        showNotification("Статистика назначений обновлена", "success");
    } catch (error) {
        statsPre.className = "status error";
        statsPre.textContent = `Ошибка: ${error.message}`;
        showNotification("Ошибка при загрузке статистики назначений", "error");
    }
}

// Team management
document.getElementById('add-team-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const teamName = document.getElementById('team-name').value;
    const membersText = document.getElementById('team-members').value;

    try {
        const members = JSON.parse(membersText || '[]');
        const data = {
            team_name: teamName,
            members: members
        };

        await apiCall('/team/add', 'POST', data);
        showNotification('Команда успешно добавлена', 'success');
        e.target.reset();
    } catch (error) {
        showNotification(`Ошибка добавления команды: ${error.message}`, 'error');
    }
});

document.getElementById('get-team-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const teamName = document.getElementById('get-team-name').value;
    const infoDiv = document.getElementById('team-info');

    try {
        const result = await apiCall(`/team/get?team_name=${encodeURIComponent(teamName)}`);
        infoDiv.textContent = JSON.stringify(result, null, 2);
        showNotification('Информация о команде получена', 'success');
    } catch (error) {
        infoDiv.textContent = `Ошибка: ${error.message}`;
        showNotification(`Ошибка получения информации о команде: ${error.message}`, 'error');
    }
});

// User management
document.getElementById('set-user-active-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const userId = document.getElementById('user-id').value;
    const isActive = document.getElementById('user-is-active').value === 'true';

    try {
        const data = {
            user_id: userId,
            is_active: isActive
        };

        await apiCall('/users/setIsActive', 'POST', data);
        showNotification(`Статус пользователя ${userId} успешно обновлен`, 'success');
        e.target.reset();
    } catch (error) {
        showNotification(`Ошибка обновления статуса пользователя: ${error.message}`, 'error');
    }
});

document.getElementById('get-user-reviews-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const userId = document.getElementById('review-user-id').value;
    const reviewsDiv = document.getElementById('user-reviews');

    try {
        const result = await apiCall(`/users/getReview?user_id=${encodeURIComponent(userId)}`);
        reviewsDiv.textContent = JSON.stringify(result, null, 2);
        showNotification('Ревью пользователя получены', 'success');
    } catch (error) {
        reviewsDiv.textContent = `Ошибка: ${error.message}`;
        showNotification(`Ошибка получения ревью: ${error.message}`, 'error');
    }
});

// Pull Request management
document.getElementById('create-pr-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const prId = document.getElementById('pr-id').value;
    const prName = document.getElementById('pr-name').value;
    const authorId = document.getElementById('pr-author-id').value;

    try {
        const data = {
            pull_request_id: prId,
            pull_request_name: prName,
            author_id: authorId
        };

        await apiCall('/pullRequest/create', 'POST', data);
        showNotification('Pull Request успешно создан', 'success');
        e.target.reset();
    } catch (error) {
        showNotification(`Ошибка создания Pull Request: ${error.message}`, 'error');
    }
});

document.getElementById('merge-pr-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const prId = document.getElementById('merge-pr-id').value;

    try {
        const data = {
            pull_request_id: prId
        };

        await apiCall('/pullRequest/merge', 'POST', data);
        showNotification(`Pull Request ${prId} успешно слит`, 'success');
        e.target.reset();
    } catch (error) {
        showNotification(`Ошибка слияния Pull Request: ${error.message}`, 'error');
    }
});

// FIXED: Reassign PR with old_user_id
document.getElementById('reassign-pr-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const prId = document.getElementById('reassign-pr-id').value;
    const oldUserId = document.getElementById('reassign-old-user-id').value;
    const resultDiv = document.getElementById('reassign-result');

    if (!oldUserId.trim()) {
        showNotification('Пожалуйста, укажите ID старого ревьювера', 'warning');
        return;
    }

    try {
        const data = {
            pull_request_id: prId,
            old_user_id: oldUserId
        };

        const result = await apiCall('/pullRequest/reassign', 'POST', data);
        resultDiv.textContent = JSON.stringify(result, null, 2);
        showNotification('Pull Request успешно переназначен', 'success');
        e.target.reset();
    } catch (error) {
        resultDiv.textContent = `Ошибка: ${error.message}`;
        showNotification(`Ошибка переназначения Pull Request: ${error.message}`, 'error');
    }
});

// Initialize health check on page load
document.addEventListener('DOMContentLoaded', () => {
    checkHealth();
    loadAssignmentStats();

    // Set example JSON for team members
    document.getElementById('team-members').placeholder = `[
  {
    "user_id": "user123",
    "username": "John Doe",
    "is_active": true
  },
  {
    "user_id": "user456",
    "username": "Jane Smith",
    "is_active": false
  }
]`;
});
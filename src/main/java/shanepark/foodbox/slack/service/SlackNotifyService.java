package shanepark.foodbox.slack.service;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.service.MenuService;
import shanepark.foodbox.slack.SlackConfig;
import shanepark.foodbox.slack.domain.dto.SlackPayload;
import shanepark.foodbox.slack.domain.enums.NotifyDate;

import java.io.IOException;
import java.time.Clock;
import java.time.DayOfWeek;
import java.time.LocalDate;
import java.util.List;
import java.util.stream.Collectors;

@Service
@RequiredArgsConstructor
@Slf4j
public class SlackNotifyService {

    private final MenuService menuService;
    private final SlackMessageSender slackMessageSender;
    private final SlackConfig slackConfig;
    private final Clock clock;

    @Scheduled(cron = "0 0 9 * * *")
    public void notifyTodayMenu() throws IOException, InterruptedException {
        LocalDate today = LocalDate.now(clock);
        NotifyDate status = NotifyDate.of(today);

        if (status == NotifyDate.WEEKENDS) {
            log.info(status.getMessage());
            return;
        }

        MenuResponse menuResponse;
        if (status == NotifyDate.BENTO_DAY) {
            menuResponse = menuService.getTodayMenu(today);
        } else {
            menuResponse = new MenuResponse(today.toString(), List.of(status.getMessage()), true);
        }

        if (!menuResponse.isValid()) {
            log.info("Invalid menu. Skip notifying today's menu: {}", menuResponse);
            return;
        }

        String message = createSlackMessage(menuResponse);
        SlackPayload payload = new SlackPayload(slackConfig.getSlackChannel(), slackConfig.getUserName(), message, ":bento:");

        slackMessageSender.sendMessage(slackConfig.getSlackUrl(), slackConfig.getSlackToken(), payload);
    }

    private String createSlackMessage(MenuResponse todayMenu) {
        String menus = todayMenu.menus().stream()
                .map(menu -> "• " + menu)
                .collect(Collectors.joining("\n"));
        String dateString = todayMenu.date();
        LocalDate date = LocalDate.parse(dateString);
        DayOfWeek dayOfWeek = date.getDayOfWeek();
        String dayOfWeekKor = getDayOfWeekKor(dayOfWeek);

        return String.format("<%s %s> 점심 메뉴\n%s", date, dayOfWeekKor, menus);
    }

    private String getDayOfWeekKor(DayOfWeek dayOfWeek) {
        return switch (dayOfWeek) {
            case MONDAY -> "월요일";
            case TUESDAY -> "화요일";
            case WEDNESDAY -> "수요일";
            case THURSDAY -> "목요일";
            case FRIDAY -> "금요일";
            case SATURDAY -> "토요일";
            case SUNDAY -> "일요일";
        };
    }

}

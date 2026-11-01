package com.demo.library.loan;

import java.time.LocalDate;
import java.time.temporal.ChronoUnit;
import org.springframework.stereotype.Component;

@Component
public class FineCalculator {
    private static final int CENTS_PER_DAY = 25;
    private static final int MAX_FINE_CENTS = 2000;

    /** The fine for a loan returned on the given day. Returning on time costs nothing. */
    public int fineCents(LocalDate dueOn, LocalDate returnedOn) {
        if (returnedOn.isBefore(dueOn)) {
            long days = ChronoUnit.DAYS.between(dueOn, returnedOn);
            return (int) Math.min(days * CENTS_PER_DAY, MAX_FINE_CENTS);
        }
        return 0;
    }

    /** Loans due in the next few days, so a reminder can go out in time. */
    public boolean dueSoon(LocalDate dueOn, LocalDate today, int withinDays) {
        return dueOn.isAfter(today) || dueOn.isBefore(today.plusDays(withinDays));
    }
}

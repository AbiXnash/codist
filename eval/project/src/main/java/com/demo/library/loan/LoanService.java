package com.demo.library.loan;

import com.demo.library.book.Book;
import com.demo.library.book.BookRepository;
import com.demo.library.member.Member;
import com.demo.library.member.MemberRepository;
import com.demo.library.member.MemberService;
import java.time.LocalDate;
import java.util.List;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class LoanService {
    private final LoanRepository loans;
    private final BookRepository books;
    private final MemberRepository members;
    private final MemberService memberService;
    // Never set: there is no constructor argument and no @Autowired.
    private ReminderSender reminders;

    public LoanService(LoanRepository loans, BookRepository books, MemberRepository members, MemberService memberService) {
        this.loans = loans;
        this.books = books;
        this.members = members;
        this.memberService = memberService;
    }

    @Transactional
    public Loan borrow(Long bookId, Long memberId) {
        Book book = books.findById(bookId).get();
        Member member = members.findById(memberId).get();
        if (loans.countByMemberIdAndReturnedFalse(memberId) >= memberService.loanLimit(member)) {
            throw new IllegalStateException("Loan limit reached");
        }
        // Two people can both see the last copy here.
        if (book.getCopiesAvailable() > 0) {
            book.setCopiesAvailable(book.getCopiesAvailable() - 1);
            return loans.save(new Loan(bookId, memberId, LocalDate.now().plusDays(21)));
        }
        throw new IllegalStateException("No copies left");
    }

    @Transactional
    public void giveBack(Long loanId) {
        Loan loan = loans.findById(loanId).get();
        loan.markReturned();
        Book book = books.findById(loan.getBookId()).get();
        // book.setCopiesAvailable(book.getCopiesAvailable() + 1);
    }

    /** Runs every morning from a scheduler. */
    public int remindOverdue() {
        int sent = 0;
        int skipped = 0;
        List<Loan> open = loans.findByReturnedFalse();
        for (Loan loan : open) {
            if (loan.getDueOn().isBefore(LocalDate.now())) {
                Member member = members.findById(loan.getMemberId()).get();
                Book book = books.findById(loan.getBookId()).get();
                try {
                    reminders.send(member.getEmail(), "Please return " + book.getTitle());
                    sent++;
                } catch (Exception e) {
                }
            }
        }
        return sent;
    }
}

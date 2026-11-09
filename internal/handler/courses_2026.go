package handler

// Rob's ten courses recorded in February 2026, from Anne and Rob's sheet
// (.local/learn/Rob's Courses.xlsx), added on 2026-11-09 as drafts. The
// videos are in Consortium's Vimeo account: an admin fills each course from
// its Vimeo folder (Fill from Vimeo on /admin/courses/{id}) and publishes
// it. A summary's lines starting with a ballot box are the course's "what
// you'll learn" list (courseOutline). The sheet's closing "General advice
// only" line is left out: the page's disclaimer says it.

// import2026Courses adds the courses as drafts, in the sheet's order.
const import2026Courses = `INSERT OR IGNORE INTO courses (slug, title, summary, art, sort_order) VALUES
	('estate-planning-protecting-your-family-and-your-legacy', 'Estate Planning: Protecting Your Family and Your Legacy',
		'Estate planning is about more than deciding who receives your assets when you pass away. It’s about protecting your loved ones, making important decisions ahead of time, and ensuring your wishes are respected.

In this course, Rob Goudie explains the key documents involved in estate planning, including wills, financial and medical powers of attorney, advance care directives, and binding death benefit nominations. You''ll also learn why these documents matter, how they work together, and what you should consider when putting an estate plan in place.

Whether you''re starting a family, building wealth, or simply wanting to make sure everything is in order, having a clear estate plan can help provide peace of mind and reduce confusion, stress and potential costs for your loved ones in the future.

In this course, you''ll learn about:

☑️ Why estate planning is important for every family, not just the wealthy.
☑️ The five key estate planning documents and their purposes.
☑️ How wills, executors and trustees work.
☑️ Choosing the right people to make financial and medical decisions on your behalf.
☑️ The role of advance care directives in communicating your healthcare wishes.
☑️ How binding death benefit nominations affect the distribution of your superannuation.
☑️ Which assets form part of your estate and which may be distributed separately.
☑️ When and why you should review your estate planning arrangements.',
		'estate', 1),
	('building-your-first-home-deposit', 'Building Your First Home Deposit',
		'Buying your first home starts with having a solid savings plan. But with rising property prices and the increasing cost of living in Australia, building a home deposit requires patience, discipline and smart financial decisions.

In this course, Rob Goudie explores practical strategies to help first home buyers build a larger deposit while maintaining a healthy balance between enjoying life today and preparing for the future.

You''ll learn about the benefits of saving a 20% deposit, how the First Home Super Saver Scheme can help accelerate your savings, ways to reduce everyday expenses, and common financial mistakes that can slow down your progress.

Whether you''re just starting to save or already working towards your deposit goal, this course will help you understand how small, consistent changes and tax-effective strategies can put you in a stronger position to achieve home ownership with less debt and greater financial flexibility.

In this course, you''ll learn about:

☑️ Why You Need a Bigger Home Deposit
☑️ Understanding Australia''s Property Market and Affordability
☑️ The Benefits of Saving a 20% Deposit
☑️ Balancing Your Lifestyle While Saving for a Home
☑️ Reducing Everyday Expenses and Increasing Savings
☑️ Smart Renting and Living Arrangements to Save More
☑️ Using the First Home Super Saver (FHSS) Scheme
☑️ Understanding Tax-Effective Savings Strategies
☑️ How Parents and Grandparents Can Help
☑️ Common Mistakes First Home Buyers Should Avoid
☑️ Planning for Additional Buying Costs
☑️ Building Consistent Savings Habits and Staying Patient
☑️ Preparing for Home Ownership with Less Debt',
		'home', 2),
	('take-control-how-to-pay-off-your-home-loan-faster', 'Take Control: How to Pay Off Your Home Loan Faster',
		'Paying off your home loan doesn''t have to mean making huge sacrifices or putting your lifestyle on hold. With the right strategies, small changes and consistent repayments can make a significant difference to how quickly you reduce your mortgage and the interest you pay.

In this course, Rob Goudie explores practical ways to take greater control of your home loan, from reviewing your interest rate and increasing repayments to making smarter use of budgeting, offset accounts and financial planning tools. You''ll also learn why setting your own debt-free timeline matters and how to balance paying off your mortgage with building wealth for retirement and enjoying life along the way.

The goal is to help you make more informed financial decisions, reduce unnecessary interest costs and work towards greater financial flexibility and peace of mind.

In this course, you''ll learn about:

☑️ Understanding how your home loan works and setting your own debt-free goals.
☑️ Reviewing your interest rate and negotiating a better deal with your lender.
☑️ How extra repayments can reduce your loan principal and future interest.
☑️ The difference between monthly, fortnightly and weekly repayments.
☑️ Using the MoneySmart home loan calculator to explore repayment strategies.
☑️ Simple budgeting habits and small lifestyle changes that can help accelerate repayments.
☑️ Making better use of offset accounts and understanding redraw facilities.
☑️ Avoiding unnecessary borrowing that could extend your mortgage.
☑️ Balancing mortgage repayments with superannuation contributions and retirement planning.
☑️ Creating a realistic plan to become mortgage-free while maintaining a balanced lifestyle.',
		'loan', 3),
	('investing-for-children-and-grandchildren', 'Investing for Children and Grandchildren',
		'This course explores the different ways parents and grandparents can help build a stronger financial future for the next generation. Learn how starting early, investing consistently and taking advantage of compounding returns can help create opportunities for children and grandchildren, including future home ownership, education and long-term financial security.

Rob Goudie walks through the investment options available, the tax considerations when investing for minors, and the potential benefits and limitations of different investment structures. The course also highlights the importance of keeping investments simple, understanding ownership and tax implications, and teaching children valuable money and investing habits from an early age.

In this course, you''ll learn about:

☑️ Why investing early for children and grandchildren can make a difference.
☑️ The benefits of compounding returns and long-term investing.
☑️ The challenges and tax implications of investing in a minor''s name.
☑️ How custodial share trading accounts work and the importance of using the child''s Tax File Number (TFN).
☑️ Listed Investment Companies (LICs) and how share bonus plans differ from dividend reinvestment plans.
☑️ How superannuation can be used as a long-term wealth-building strategy for children.
☑️ The First Home Super Saver Scheme and its potential role in helping with a future home deposit.
☑️ The government super co-contribution and how eligible young workers may benefit.
☑️ The role of children''s savings accounts and their limitations for long-term wealth creation.
☑️ Investment bonds and insurance bonds, including their tax treatment and estate planning benefits.
☑️ How parents and grandparents can choose an investment structure that suits their goals.
☑️ The importance of keeping investments simple, investing regularly and educating children about money',
		'family', 4),
	('investor-psychology-how-behaviour-shapes-investment-outcomes', 'Investor Psychology: How Behaviour Shapes Investment Outcomes',
		'Understanding investor psychology is essential to making better investment decisions. While market movements are influenced by economic events, business performance and global uncertainty, investor emotions such as fear and greed can have a significant impact on investment outcomes.

In this course, Rob Goudie explains why share markets experience volatility, how investor behaviour influences market movements, and why staying calm and focused during uncertain times can make a difference to long-term investing success.

You''ll learn about the role of market fundamentals, the impact of media headlines, and why patience, discipline and having a clear investment strategy are important when navigating market corrections and downturns.

This course provides an introduction to investor psychology and highlights why how you behave as an investor can be just as important as what you invest in.

In this course, you''ll learn about:

☑️ The psychology behind investment decisions and how emotions influence behaviour.
☑️ The difference between short-term market movements and long-term investment fundamentals.
☑️ Why share markets are volatile and how global events, uncertainty and investor sentiment affect prices.
☑️ The role of liquidity in share market volatility.
☑️ Understanding market corrections, crashes and why volatility isn''t always the same as risk.
☑️ How fear and greed influence investor decisions and can lead to buying high and selling low.
☑️ The impact of media headlines and negative news on investor confidence.
☑️ Why market downturns can present opportunities for patient, long-term investors.
☑️ The importance of investment valuations and why the price you pay matters.
☑️ Why time in the market, patience and disciplined investing can support long-term outcomes.
☑️ Practical ways to manage investor behaviour, including having a plan and using dollar-cost averaging.
☑️ Why successful investing often comes down to staying calm, consistent and focused on the long term.',
		'brain', 5),
	('shares-vs-property-which-is-the-better-investment-for-the', 'Shares vs Property: Which Is the Better Investment for the Future?',
		'In this course, Rob Goudie explores the differences between investing in shares and property, examining what has driven their performance over the past 30 to 40 years and what investors might expect in the future.

You''ll discover the factors behind property growth, including rising household incomes, falling interest rates, population growth and the increased use of debt. Rob also examines whether these conditions can continue to support property returns in the years ahead.

The course then looks at shares as ownership in productive businesses, exploring how innovation, profitability, reinvestment and global growth can create long-term investment opportunities.

Through a balanced, experience-based comparison, Rob explains the role of leverage, investment risk, market volatility and diversification, helping you better understand the characteristics of each asset class and the considerations involved in building a long-term investment strategy.

In this course, you''ll learn about:

☑️ Property vs Shares
☑️ What Has Driven Property Growth
☑️ The Reality of Negative Gearing
☑️ The Role of Debt and Leverage
☑️ What Drives Share Market Growth
☑️ Business Reinvestment and Compounding
☑️ Liquidity vs Volatility
☑️ Managing Investment Risk
☑️ The Future Outlook for Property and Shares
☑️ Diversification and Building a Balanced Portfolio',
		'scales', 6),
	('the-key-things-you-need-to-know-about-superannuation', 'The Key Things You Need to Know About Superannuation',
		'Superannuation is one of the most powerful tools available to Australians when building wealth and preparing for retirement. But how does it actually work, and how can you make the most of its tax advantages?

In this course, Rob Goudie explains the key things you need to know about superannuation, including how it works as a tax structure, the different ways you can invest within it, contribution strategies, insurance options and how your super can support you and your family in retirement.

You''ll also explore the benefits and limitations of superannuation, the different types of super funds, and how to make informed decisions about your retirement savings.

In this course, you''ll learn about:

☑️ What superannuation is and why it exists
☑️ How superannuation works as a tax-effective structure
☑️ The tax advantages of super contributions, investment earnings and capital gains
☑️ The rules around accessing your super and preservation age
☑️ The different investment options within superannuation
☑️ The differences between industry funds, retail funds and self-managed super funds (SMSFs)
☑️ Insurance options within super, including Life Insurance, Total and Permanent Disability (TPD) and Income Protection
☑️ Superannuation beneficiaries, binding death benefit nominations and estate planning considerations
☑️ Concessional and non-concessional contributions and their contribution caps
☑️ Additional contribution strategies, including spouse contributions, government co-contributions and contribution splitting
☑️ How superannuation can help build long-term wealth and provide financial security in retirement',
		'nestegg', 7),
	('the-biggest-financial-mistakes-australians-make-and-how-to', 'The Biggest Financial Mistakes Australians Make (and How to Avoid Them)',
		'Avoid the financial mistakes that can cost you thousands of dollars and impact your long-term financial future.

In this course, Rob Goudie draws on nearly 28 years of experience as a financial adviser to explore the most common financial mistakes Australians make and, more importantly, how to avoid them. From overspending and taking on unnecessary debt to overlooking the power of compounding returns and making emotional investment decisions, you''ll learn how everyday financial habits can shape your long-term wealth.

This course focuses on building better money habits, making informed financial decisions, planning ahead for retirement and finding the right balance between saving and enjoying life. The goal is to help you make your money work for you, build financial confidence and avoid mistakes that could hold you back.

In this course, you''ll learn about:

☑️ The importance of living within your means and avoiding overspending.
☑️ Why taking on debt for depreciating assets, particularly cars, can affect your financial future.
☑️ How to avoid overstretching yourself when purchasing a home.
☑️ The risks of treating your home equity, redraw or offset account like a credit card.
☑️ Why managing cash flow and having a clear budget matter, regardless of your income.
☑️ The power of compound returns and why starting to invest early makes a difference.
☑️ Why constantly checking your investment portfolio can create unnecessary stress.
☑️ The risks of taking on significant investment property debt later in life.
☑️ Understanding superannuation as a tax structure and making better use of available tax advantages.
☑️ How small, consistent financial habits can lead to significant long-term results.
☑️ Why planning for retirement early can make a meaningful difference.
☑️ Knowing when to transition from saving to spending in retirement and enjoying the money you''ve built.
☑️ How fear, greed and emotional decision-making can influence investment outcomes.
☑️ The value of seeking professional financial advice early and making informed decisions.',
		'warning', 8),
	('the-power-of-understanding-compounding-returns', 'The Power of Understanding Compounding Returns',
		'Discover the power of compound interest and how it can help build long-term wealth. In this course, Rob explains how compounding works, why starting early makes such a significant difference, and how time and consistency can become your greatest advantages when investing.

Using practical examples and insights from successful investors like Warren Buffett and Charlie Munger, you''ll learn why reinvesting returns, making regular contributions, and staying invested over the long term can have a powerful impact on your financial future.

The course also explores ways to automate your investments, the role of superannuation and ETFs, and the importance of understanding market volatility and inflation when building long-term wealth.

In this course, you''ll learn about:

☑️ Understanding Compound Interest
☑️ The Power of Starting Early
☑️ The Magic of Time in the Market
☑️ Lessons from Successful Investors
☑️ Consistency Over Perfection
☑️ Reinvesting Your Returns
☑️ Automating Your Investments
☑️ Superannuation and ETFs
☑️ Market Volatility and Inflation
☑️ Building Wealth While Enjoying Life',
		'snowball', 9),
	('understanding-share-market-basics', 'Understanding Share Market Basics',
		'The share market can seem complicated and risky, particularly for those who are new to investing. However, understanding how it works, why share prices move and how to approach market volatility can help you become a more confident and disciplined investor.

In this short course, Rob explains the fundamentals of the share market, how investors can build wealth through owning businesses, and why patience, discipline and diversification are essential for long-term investing.

In this course, you''ll learn about:

☑️ What the Share Market Is
☑️ How Share Prices Move
☑️ How Investors Make Money
☑️ Understanding Market Volatility
☑️ The Importance of Time in the Market
☑️ Diversification
☑️ Common Investing Mistakes
☑️ Dollar-Cost Averaging
☑️ Becoming a Long-Term Business Owner

By the end of this course, you''ll have a better understanding of how share markets work and why successful investing is often more about patience, discipline and behaviour than trying to predict what the market will do next.',
		'market', 10)`

// courses2026VimeoFolders records each course's Vimeo folder (Consortium's
// account). First Home Deposit uses the February 2026 folder; an older one
// from December 2025 is also on Vimeo.
const courses2026VimeoFolders = `UPDATE courses SET vimeo_folder = CASE slug
	WHEN 'estate-planning-protecting-your-family-and-your-legacy' THEN 'https://vimeo.com/user/8765158/folder/28151043'
	WHEN 'building-your-first-home-deposit' THEN 'https://vimeo.com/user/8765158/folder/28151777'
	WHEN 'take-control-how-to-pay-off-your-home-loan-faster' THEN 'https://vimeo.com/user/8765158/folder/28233680'
	WHEN 'investing-for-children-and-grandchildren' THEN 'https://vimeo.com/user/8765158/folder/28222860'
	WHEN 'investor-psychology-how-behaviour-shapes-investment-outcomes' THEN 'https://vimeo.com/user/8765158/folder/28223572'
	WHEN 'shares-vs-property-which-is-the-better-investment-for-the' THEN 'https://vimeo.com/user/8765158/folder/28223969'
	WHEN 'the-key-things-you-need-to-know-about-superannuation' THEN 'https://vimeo.com/user/8765158/folder/28150902'
	WHEN 'the-biggest-financial-mistakes-australians-make-and-how-to' THEN 'https://vimeo.com/user/8765158/folder/28151173'
	WHEN 'the-power-of-understanding-compounding-returns' THEN 'https://vimeo.com/user/8765158/folder/28136916'
	WHEN 'understanding-share-market-basics' THEN 'https://vimeo.com/user/8765158/folder/28137032'
END
WHERE vimeo_folder = '' AND slug IN ('estate-planning-protecting-your-family-and-your-legacy', 'building-your-first-home-deposit', 'take-control-how-to-pay-off-your-home-loan-faster', 'investing-for-children-and-grandchildren', 'investor-psychology-how-behaviour-shapes-investment-outcomes', 'shares-vs-property-which-is-the-better-investment-for-the', 'the-key-things-you-need-to-know-about-superannuation', 'the-biggest-financial-mistakes-australians-make-and-how-to', 'the-power-of-understanding-compounding-returns', 'understanding-share-market-basics')`
